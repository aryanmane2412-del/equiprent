// Package logger builds the process-wide structured logger (log/slog).
package logger

import (
	"io"
	"log/slog"

	"github.com/equiprent/backend/internal/config"
)

// New returns a slog.Logger writing text (development) or JSON (deployed
// environments) to w. Every record carries the service name and environment.
func New(w io.Writer, cfg config.LogConfig, service string, env config.Environment) *slog.Logger {
	opts := &slog.HandlerOptions{Level: cfg.Level}
	var h slog.Handler
	if cfg.Format == "json" {
		h = slog.NewJSONHandler(w, opts)
	} else {
		h = slog.NewTextHandler(w, opts)
	}
	return slog.New(h).With(slog.String("service", service), slog.String("env", string(env)))
}

// Discard returns a logger that drops everything; useful in tests.
func Discard() *slog.Logger { return slog.New(slog.DiscardHandler) }
