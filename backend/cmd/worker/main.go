// Command worker runs the background jobs (docs/ARCHITECTURE.md §5.4):
// outbox → notifications, booking-request expiry, work-log auto-approval,
// key/OTP cleanup, location partition upkeep, document-expiry and
// maintenance-due scans. Jobs are single-flight across replicas (advisory
// locks) and the process shuts down gracefully on SIGINT/SIGTERM.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/equiprent/backend/internal/app"
	"github.com/equiprent/backend/internal/bootstrap"
	"github.com/equiprent/backend/internal/db"
	"github.com/equiprent/backend/internal/health"
	"github.com/equiprent/backend/internal/worker"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "worker:", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, log, err := bootstrap.Init("equiprent-worker")
	if err != nil {
		return err
	}

	deps, err := bootstrap.Connect(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer func() {
		if err := deps.Close(); err != nil {
			log.Warn("closing dependencies", "error", err)
		}
	}()

	if info, err := db.New(deps.DB).GetDatabaseInfo(ctx); err == nil {
		log.Info("database info", "database", info.DatabaseName, "server_version", info.ServerVersion)
	}

	a := app.New(cfg, log, deps.DB, deps.Store())
	jobs := worker.Jobs{Pool: deps.DB, Dispatcher: a.Notifications, Refunds: a.Payments, Tracking: a.Tracking, Log: log,
		ProcessingTimeout: cfg.Booking.ProcessingTimeout}.All()
	jobs = append(jobs, worker.Job{
		Name: "heartbeat", Interval: cfg.Worker.HeartbeatInterval,
		Run: func(ctx context.Context) error {
			report := deps.Health(cfg).Ready(ctx)
			stat := deps.DB.Stat()
			log.Debug("worker heartbeat", "dependencies", report.Status,
				"db_total_conns", stat.TotalConns(), "db_idle_conns", stat.IdleConns())
			if report.Status != health.StatusUp {
				log.Warn("worker dependencies degraded", "checks", report.Checks)
			}
			return nil
		},
	})

	sched := worker.NewScheduler(worker.NewPGLocker(deps.DB, a.Tx), log, jobs...)
	names := make([]string, len(jobs))
	for i, j := range jobs {
		names[i] = j.Name
	}
	log.Info("worker started", "jobs", names)
	sched.Run(ctx) // returns after in-flight runs finish
	log.Info("worker stopped")
	return nil
}
