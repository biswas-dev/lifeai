// Package version carries build metadata set via -ldflags at build time.
package version

// Set at build time.
var (
	Version   = "dev"
	GitCommit = "unknown"
	BuildTime = "unknown"
)

// Info is the shape /api/version returns.
type Info struct {
	Version   string `json:"version"`
	GitCommit string `json:"git_commit"`
	BuildTime string `json:"build_time"`
}

// Get returns the build metadata.
func Get() Info {
	return Info{Version: Version, GitCommit: GitCommit, BuildTime: BuildTime}
}
