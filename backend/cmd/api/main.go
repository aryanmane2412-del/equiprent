// Command api runs the EQUIPRENT REST API (Gin) on HTTP_PORT.
//
//	api              serve
//	api healthcheck  probe /healthz (Docker HEALTHCHECK)
//	api openapi      print the OpenAPI 3.1 document (no database needed)
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/equiprent/backend/internal/app"
	"github.com/equiprent/backend/internal/bootstrap"
	"github.com/equiprent/backend/internal/config"
	"github.com/equiprent/backend/internal/database"
	"github.com/equiprent/backend/internal/httpserver"
	"github.com/equiprent/backend/internal/logger"
)

func main() {
	// `api healthcheck` probes the liveness endpoint; used by the Docker
	// HEALTHCHECK because the distroless image has no curl/wget.
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}
	if len(os.Args) > 1 && os.Args[1] == "openapi" {
		os.Exit(printOpenAPI())
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "api:", err)
		os.Exit(1)
	}
}

func healthcheck() int {
	port := os.Getenv("HTTP_PORT")
	if port == "" {
		port = "8080"
	}
	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://127.0.0.1:" + port + httpserver.PathLiveness)
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "healthcheck: status", resp.StatusCode)
		return 1
	}
	return 0
}

// printOpenAPI writes the spec to stdout. The modules are wired against a
// lazy pool that never connects, so no database is needed.
func printOpenAPI() int {
	cfg, err := config.Load(func(k string) (string, bool) {
		if k == "DATABASE_URL" {
			return "postgres://docs@localhost:1/docs", true
		}
		return os.LookupEnv(k)
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "openapi:", err)
		return 1
	}
	pool, err := database.NewPool(context.Background(), cfg.Database, "openapi")
	if err != nil {
		fmt.Fprintln(os.Stderr, "openapi:", err)
		return 1
	}
	defer pool.Close()
	a := app.New(cfg, logger.Discard(), pool, nil)
	doc, err := httpserver.BuildSpec(httpserver.Deps{Config: cfg, Modules: a.Modules, Guards: a.Guards})
	if err != nil {
		fmt.Fprintln(os.Stderr, "openapi:", err)
		return 1
	}
	_, _ = os.Stdout.Write(append(doc, byte(10)))
	return 0
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, log, err := bootstrap.Init("")
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

	store := deps.Store()
	log.Info("key-value store", "backend", store.Backend())
	a := app.New(cfg, log, deps.DB, store)
	router := httpserver.NewRouter(httpserver.Deps{
		Config:  cfg,
		Logger:  log,
		Health:  deps.Health(cfg),
		Modules: a.Modules,
		Guards:  a.Guards,
		Limiter: a.Limiter,
	})
	if cfg.Docs.Enabled {
		log.Info("API docs enabled", "swagger_ui", cfg.Docs.PublicURL+httpserver.PathDocs, "openapi", cfg.Docs.PublicURL+httpserver.PathOpenAPI)
	}
	srv := httpserver.NewServer(cfg.HTTP, router, log)

	if err := httpserver.Run(ctx, srv, cfg.ShutdownTimeout, log); err != nil {
		return err
	}
	log.Info("api stopped")
	return nil
}
