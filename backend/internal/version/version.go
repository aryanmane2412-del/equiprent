// Package version exposes build metadata injected at link time:
//
//	go build -ldflags "-X github.com/equiprent/backend/internal/version.Version=1.2.3 \
//	  -X github.com/equiprent/backend/internal/version.Commit=$(git rev-parse --short HEAD)"
package version

var (
	Version   = "dev"
	Commit    = "none"
	BuildTime = "unknown"
)
