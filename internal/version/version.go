// Package version holds build-time version metadata for the sonar binary.
// Values are injected at compile time via ldflags:
//
//	go build -ldflags "-X github.com/samuelbailey123/sonar/internal/version.Version=1.0.0 \
//	  -X github.com/samuelbailey123/sonar/internal/version.Commit=$(git rev-parse --short HEAD) \
//	  -X github.com/samuelbailey123/sonar/internal/version.Date=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
package version

import (
	"fmt"
	"runtime"
)

// These variables are set at build time via -ldflags. When not set (e.g. during
// development with a plain `go run`), they fall back to sensible defaults.
var (
	// Version is the semantic version of the build (e.g. "0.1.0").
	Version = "dev"

	// Commit is the short SHA of the git commit used for the build.
	Commit = "unknown"

	// Date is the UTC timestamp of the build in ISO 8601 format.
	Date = "unknown"
)

// Full returns a single-line version string suitable for CLI --version output.
func Full() string {
	return fmt.Sprintf("sonar %s (commit %s, built %s, %s/%s)",
		Version, Commit, Date, runtime.GOOS, runtime.GOARCH,
	)
}

// Short returns just the semantic version string.
func Short() string {
	return Version
}
