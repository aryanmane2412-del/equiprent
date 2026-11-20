package httpserver

import (
	"log/slog"

	"github.com/gin-gonic/gin"

	"github.com/equiprent/backend/internal/httpx"
)

// Problem is the RFC 9457 error body used by every endpoint.
type Problem = httpx.Problem

// Fail records err on the context and aborts the chain; see httpx.Fail.
func Fail(c *gin.Context, err error) { httpx.Fail(c, err) }

func writeProblem(c *gin.Context, log *slog.Logger, err error) { httpx.WriteProblem(c, log, err) }
