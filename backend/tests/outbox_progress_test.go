package tests

import (
	"context"
	"encoding/json"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/equiprent/backend/internal/logger"
	"github.com/equiprent/backend/internal/worker"
)

// slowDispatcher delivers each event after a delay, like a busy database or a slow notification
// channel. It honours the job's deadline the way real database calls do.
type slowDispatcher struct {
	delay     time.Duration
	delivered atomic.Int32
}

func (d *slowDispatcher) Dispatch(ctx context.Context, _ string, _ uuid.UUID, _ json.RawMessage) (int, error) {
	select {
	case <-time.After(d.delay):
	case <-ctx.Done():
		return 0, ctx.Err()
	}
	d.delivered.Add(1)
	return 1, nil
}

// When events take longer to deliver than a whole batch allows within the job's time limit, every run
// must still make progress. A batch that is rolled back as a whole on timeout delivers nothing, then
// claims the same events again and times out again: notifications stop for everyone.
func TestSlowOutboxStillMakesProgressEveryRun(t *testing.T) {
	e := newIsolatedEnv(t)
	const events = 40
	for range events {
		e.exec(`INSERT INTO outbox_events (aggregate_type, aggregate_id, event_type, payload) VALUES ('test', $1, 'test.event', '{}')`, uuid.New())
	}
	disp := &slowDispatcher{delay: 100 * time.Millisecond}
	sched := worker.NewScheduler(worker.NewPGLocker(e.pool, e.app.Tx), logger.Discard())
	var job worker.Job
	for _, j := range (worker.Jobs{Pool: e.pool, Dispatcher: disp, Log: logger.Discard()}).All() {
		if j.Name == "outbox.dispatch" {
			job = j
		}
	}
	job.Timeout = 1500 * time.Millisecond // a full batch of 100 events needs ~10 s

	pending := func() int {
		n, err := strconv.Atoi(e.query1(`SELECT count(*) FROM outbox_events WHERE published_at IS NULL`))
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	left := pending()
	for run := 1; run <= 12 && left > 0; run++ {
		sched.RunOnce(context.Background(), job)
		now := pending()
		if now >= left {
			t.Fatalf("run %d delivered nothing (%d events still waiting): a slow backlog must not stall the queue", run, now)
		}
		left = now
	}
	if left != 0 {
		t.Fatalf("%d of %d events were still undelivered after 12 runs", left, events)
	}
	if got := int(disp.delivered.Load()); got != events {
		t.Fatalf("each event must be delivered exactly once: delivered %d of %d", got, events)
	}
}
