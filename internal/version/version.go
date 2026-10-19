// Package version holds build information, set at build time with -ldflags.
package version

var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)
