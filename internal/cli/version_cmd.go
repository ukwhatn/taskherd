package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/ukwhatn/taskherd/internal/buildinfo"
)

func (a *app) versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: a.text.CLI.Version.Short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			info := buildinfo.Get()
			if a.jsonOut {
				return a.emitJSON(struct {
					Version string `json:"version"`
					Commit  string `json:"commit,omitempty"`
					Date    string `json:"date,omitempty"`
					Go      string `json:"go"`
					OS      string `json:"os"`
					Arch    string `json:"arch"`
				}{
					Version: info.Version,
					Commit:  info.Commit,
					Date:    info.Date,
					Go:      info.Go,
					OS:      info.OS,
					Arch:    info.Arch,
				})
			}
			fmt.Fprintf(a.env.Out, "taskherd %s %s/%s\n", info, info.OS, info.Arch)
			return nil
		},
	}
}
