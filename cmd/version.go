package cmd

// Portions adapted from MeetHub (MIT): cmd/version.go

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/jniltinho/streamgo/internal/buildinfo"
)

func newVersionCmd(c *cli) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version, commit and build date",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if c.asJSON {
				return c.printJSON(map[string]string{
					"version": buildinfo.Version,
					"commit":  buildinfo.Commit,
					"date":    buildinfo.Date,
				})
			}
			_, err := fmt.Fprintf(c.out, "streamgo %s (commit %s, built %s)\n", buildinfo.Version, buildinfo.Commit, buildinfo.Date)
			return err
		},
	}
}
