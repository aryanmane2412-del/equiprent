// Package worker runs periodic background jobs. Each run happens inside one
// database transaction guarded by a PostgreSQL advisory lock, so a job runs
// on at most one worker replica at a time and its changes commit atomically.
// Runs are bounded by a timeout, panics are recovered, and shutdown waits
// for in-flight runs.
package worker

import (
	"context"
	"fmt"
	"hash/fnv"
	"log/slog"
	"math/rand/v2"
	"runtime/debug"
	"sync"
	"time"
)

// Job is a periodic task.
type Job struct {
	Name     string
	Interval time.Duration
	Timeout  time.Duration
	// Run does the work. The context carries the job's transaction.
	Run func(ctx context.Context) error
}

// Locker runs fn while holding a cluster-wide lock for key; ran=false when
// another replica holds it.
type Locker interface {
	WithLock(ctx context.Context, key int64, fn func(ctx context.Context) error) (ran bool, err error)
}

// Scheduler runs jobs on their intervals until the context ends.
type Scheduler struct {
	jobs   []Job
	locker Locker
	log    *slog.Logger
	// runs counts completed runs per job (for tests and the heartbeat log).
	mu   sync.Mutex
	runs map[string]int
}

// NewScheduler creates a scheduler.
func NewScheduler(locker Locker, log *slog.Logger, jobs ...Job) *Scheduler {
	return &Scheduler{jobs: jobs, locker: locker, log: log, runs: map[string]int{}}
}

// Runs returns how many times a job has completed successfully.
func (s *Scheduler) Runs(name string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runs[name]
}

// Run blocks until ctx is cancelled and all running jobs have returned.
func (s *Scheduler) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for _, j := range s.jobs {
		wg.Add(1)
		go func(j Job) {
			defer wg.Done()
			s.loop(ctx, j)
		}(j)
	}
	wg.Wait()
}

func (s *Scheduler) loop(ctx context.Context, j Job) {
	// Small random start delay spreads replicas and avoids a thundering herd.
	jitter := time.Duration(rand.Int64N(int64(min(j.Interval/4, 2*time.Second)) + 1))
	timer := time.NewTimer(jitter)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		s.RunOnce(ctx, j)
		timer.Reset(j.Interval)
	}
}

// RunOnce executes one run of j (exported for tests and manual triggers).
func (s *Scheduler) RunOnce(ctx context.Context, j Job) {
	timeout := j.Timeout
	if timeout <= 0 {
		timeout = j.Interval
	}
	runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()

	start := time.Now()
	ran, err := s.safeRun(runCtx, j)
	switch {
	case err != nil:
		s.log.Error("job failed", "job", j.Name, "error", err, "duration_ms", time.Since(start).Milliseconds())
	case !ran:
		s.log.Debug("job skipped: running on another worker", "job", j.Name)
	default:
		s.mu.Lock()
		s.runs[j.Name]++
		s.mu.Unlock()
		s.log.Debug("job done", "job", j.Name, "duration_ms", time.Since(start).Milliseconds())
	}
}

func (s *Scheduler) safeRun(ctx context.Context, j Job) (ran bool, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v\n%s", r, debug.Stack())
		}
	}()
	if s.locker == nil {
		return true, j.Run(ctx)
	}
	return s.locker.WithLock(ctx, LockKey(j.Name), j.Run)
}

// LockKey derives a stable advisory-lock key from a job name.
func LockKey(name string) int64 {
	h := fnv.New64a()
	h.Write([]byte("equiprent.job." + name))
	return int64(h.Sum64() & 0x7fffffffffffffff)
}
