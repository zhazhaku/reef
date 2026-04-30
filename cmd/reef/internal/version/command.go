package version

import (
	"github.com/spf13/cobra"

	"github.com/zhazhaku/reef/cmd/reef/internal"
	"github.com/zhazhaku/reef/cmd/reef/internal/cliui"
	"github.com/zhazhaku/reef/pkg/config"
)

func NewVersionCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "version",
		Aliases: []string{"v"},
		Short:   "Show version information",
		Run: func(_ *cobra.Command, _ []string) {
			printVersion()
		},
	}

	return cmd
}

func printVersion() {
	build, goVer := config.FormatBuildInfo()
	cliui.PrintVersion(internal.Logo, "reef "+config.FormatVersion(), build, goVer)
}
