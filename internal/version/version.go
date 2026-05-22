package version

import "fmt"

var (
	Version   = "dev"
	GitCommit = "unknown"
	BuildDate = "unknown"
)

type Info struct {
	Version   string `json:"version"`
	GitCommit string `json:"git_commit"`
	BuildDate string `json:"build_date"`
}

func Get() Info {
	return Info{Version: Version, GitCommit: GitCommit, BuildDate: BuildDate}
}

func (i Info) String() string {
	return fmt.Sprintf("gollm %s (commit: %s, built: %s)", i.Version, i.GitCommit, i.BuildDate)
}
