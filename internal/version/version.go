// Package version provides the application version string and git commit hash.
// These variables are set at build time via -ldflags:
//
//	go build -ldflags "-X github.com/JohnnyVBut/cascade/internal/version.Version=v1.2.3 \
//	                   -X github.com/JohnnyVBut/cascade/internal/version.GitCommit=abc1234"
//
// When built without ldflags (local `go run` / tests) the values fall back to
// "dev" / "unknown" so the binary is always functional.
package version

// Version is the application version (e.g. "v1.2.3"). Injected by ldflags.
var Version = "dev"

// GitCommit is the short git commit hash. Injected by ldflags.
var GitCommit = "unknown"
