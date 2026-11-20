// Package health runs dependency checks for the liveness and readiness
// endpoints.
package health

import (
	"context"
	"sync"
	"time"
)

type Status string

const (
	StatusUp       Status = "up"
	StatusDown     Status = "down"
	StatusDisabled Status = "disabled"
)

// Checker verifies one dependency (PostgreSQL, Redis, ...).
type Checker interface {
	Name() string
	Check(ctx context.Context) error
}

// CheckFunc adapts a function to the Checker interface.
type CheckFunc struct {
	CheckName string
	Fn        func(ctx context.Context) error
}

func (c CheckFunc) Name() string                    { return c.CheckName }
func (c CheckFunc) Check(ctx context.Context) error { return c.Fn(ctx) }

type Result struct {
	Status    Status `json:"status"`
	LatencyMS int64  `json:"latency_ms"`
	Error     string `json:"error,omitempty"`
}

type Report struct {
	Status Status            `json:"status"`
	Checks map[string]Result `json:"checks"`
}

// Service aggregates checkers. Disabled names are optional dependencies that
// are intentionally not configured (e.g. Redis in local development).
type Service struct {
	checkers     []Checker
	disabled     []string
	timeout      time.Duration
	exposeErrors bool
}

// NewService creates a readiness service. When exposeErrors is false (as in
// production) failing checks report "unavailable" instead of the raw error,
// which could leak hostnames or credentials hints.
func NewService(timeout time.Duration, exposeErrors bool, checkers ...Checker) *Service {
	return &Service{checkers: checkers, timeout: timeout, exposeErrors: exposeErrors}
}

// Disable records an optional dependency that is switched off.
func (s *Service) Disable(name string) *Service {
	s.disabled = append(s.disabled, name)
	return s
}

// Ready runs all checks concurrently, each bounded by the service timeout.
func (s *Service) Ready(ctx context.Context) Report {
	report := Report{Status: StatusUp, Checks: make(map[string]Result, len(s.checkers)+len(s.disabled))}
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, c := range s.checkers {
		wg.Add(1)
		go func(c Checker) {
			defer wg.Done()
			cctx, cancel := context.WithTimeout(ctx, s.timeout)
			defer cancel()

			start := time.Now()
			err := c.Check(cctx)
			res := Result{Status: StatusUp, LatencyMS: time.Since(start).Milliseconds()}
			if err != nil {
				res.Status = StatusDown
				res.Error = "unavailable"
				if s.exposeErrors {
					res.Error = err.Error()
				}
			}

			mu.Lock()
			report.Checks[c.Name()] = res
			if err != nil {
				report.Status = StatusDown
			}
			mu.Unlock()
		}(c)
	}
	wg.Wait()

	for _, name := range s.disabled {
		report.Checks[name] = Result{Status: StatusDisabled}
	}
	return report
}
