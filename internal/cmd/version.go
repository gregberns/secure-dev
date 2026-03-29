// Package cmd implements the version command.
// REQ-002-007: Diagnostic Commands -- version
package cmd

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
	"sd/internal/ui"
)

// Build variables set via ldflags at build time.
var (
	Version   = "dev"
	GitCommit = "unknown"
	BuildDate = "unknown"
)

// versionInfo holds the structured version output.
// REQ-002-007
type versionInfo struct {
	Version   string `json:"version"`
	GitCommit string `json:"gitCommit"`
	BuildDate string `json:"buildDate"`
	GoVersion string `json:"goVersion"`
}

func init() {
	versionCmd := &cobra.Command{
		Use:     "version",
		Short:   "Print version, commit, and build info",
		GroupID: "diagnostics",
		RunE: func(cmd *cobra.Command, args []string) error {
			info := versionInfo{
				Version:   Version,
				GitCommit: GitCommit,
				BuildDate: BuildDate,
				GoVersion: runtime.Version(),
			}

			f := Formatter()
			if f == nil {
				f = ui.NewFormatter(false)
			}

			f.SuccessData(info, func() string {
				return fmt.Sprintf("Version:    %s\nGit Commit: %s\nBuilt:      %s\nGo Version: %s\n",
					info.Version, info.GitCommit, info.BuildDate, info.GoVersion)
			})
			return nil
		},
	}

	rootCmd.AddCommand(versionCmd)
}
