// Command seed loads realistic development data (verified owners in five
// cities, listings in every category with photos, documents and prices,
// an admin and a customer) through the real API. Safe to run repeatedly.
//
//	go run ./cmd/seed        (from backend/; reads ../.env)
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/equiprent/backend/internal/app"
	"github.com/equiprent/backend/internal/bootstrap"
	"github.com/equiprent/backend/internal/config"
	"github.com/equiprent/backend/internal/httpserver"
	"github.com/equiprent/backend/internal/seed"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()
	cfg, log, err := bootstrap.Init("seed")
	if err != nil {
		return err
	}
	if cfg.Env != config.EnvDevelopment && cfg.Env != config.EnvTest {
		return errors.New("refusing to seed outside development/test (APP_ENV=" + string(cfg.Env) + ")")
	}
	if !cfg.Auth.DevSimulation {
		return errors.New("AUTH_DEV_SIMULATION must be on: seeded accounts are verified with simulated OTPs")
	}
	cfg.RateLimit.Enabled = false // many requests from one "client"

	deps, err := bootstrap.Connect(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer deps.Close()
	if err := deps.DB.Ping(ctx); err != nil {
		return fmt.Errorf("database unreachable: %w", err)
	}
	a := app.New(cfg, log, deps.DB, deps.Store())
	router := httpserver.NewRouter(httpserver.Deps{Config: cfg, Logger: log, Modules: a.Modules, Guards: a.Guards, Limiter: a.Limiter})

	rep, err := seed.Run(ctx, router, deps.DB, log)
	if err != nil {
		return err
	}
	fmt.Printf("\nSeed complete: %d listings created, %d already present.\n\n", rep.Created, rep.Skipped)
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ROLE\tNAME\tEMAIL\tPHONE")
	for _, acc := range rep.Accounts {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", acc.Role, acc.Name, acc.Email, acc.Phone)
	}
	_ = tw.Flush()
	fmt.Printf("\nPassword for every account: %s (development only).\n", seed.Password)
	return nil
}
