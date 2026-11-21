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
	"time"

	"github.com/equiprent/backend/internal/app"
	"github.com/equiprent/backend/internal/bootstrap"
	"github.com/equiprent/backend/internal/db"
	"github.com/equiprent/backend/internal/health"
	"github.com/equiprent/backend/internal/modules/settlements"
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
	gw := app.PayoutGateway(cfg)
	st := &settlements.Service{Pool: deps.DB, Tx: a.Tx, Gateway: gw, Hold: cfg.Booking.SettlementHold, Log: log,
		MaxPaise: cfg.Payout.MaxPaise, ForbidSimulated: cfg.Env.IsProduction()}
	if cfg.Payout.Live() {
		st.BankCooldown = cfg.Payout.BankCooldown // hold payouts after the payout account changed
	}
	jobs = append(jobs,
		worker.Job{Name: "settlements.create", Interval: 5 * time.Minute, Timeout: 2 * time.Minute, Run: func(c context.Context) error { _, err := st.CreateSettlements(c, 100); return err }},
		worker.Job{Name: "settlements.release", Interval: 5 * time.Minute, Run: st.ReleaseDue},
	)
	// Payout gateway (app.PayoutGateway): RazorpayX when PAYOUT_PROVIDER=razorpayx, the
	// simulator only in development without payment keys, otherwise none. The simulator
	// records owners as paid without moving money, so it never runs with real keys or in
	// production; with no gateway settlements stay "eligible" and owners are paid manually.
	if gw != nil {
		jobs = append(jobs,
			worker.Job{Name: "payouts.run", Interval: 15 * time.Minute, Timeout: 5 * time.Minute, Run: func(c context.Context) error { return st.RunPayouts(c, 200) }},
			worker.Job{Name: "payouts.settle", Interval: 2 * time.Minute, Timeout: 5 * time.Minute, Run: func(c context.Context) error { return st.SettleProcessing(c, 200) }},
			worker.Job{Name: "payouts.reconcile", Interval: time.Hour, Timeout: 5 * time.Minute, Run: func(c context.Context) error { return st.Reconcile(c, 200) }},
		)
		log.Info("automatic owner payouts are ON", "gateway", map[bool]string{true: "simulated", false: cfg.Payout.Provider}[gw.Sandbox()], "mode", cfg.Payout.Mode)
	} else {
		log.Warn("automatic owner payouts are OFF (set PAYOUT_PROVIDER=razorpayx): settlements accumulate as eligible and must be paid out manually")
	}
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
