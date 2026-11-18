package cmd

import (
	"errors"
	"fmt"
	"io/fs"

	"github.com/spf13/cobra"
)

// newLicensesCmd prints the licence texts embedded in the executable. It reads no configuration.
func newLicensesCmd(c *cli) *cobra.Command {
	return &cobra.Command{
		Use:   "licenses",
		Short: "Print the licences and notices of StreamGo and of the programs and fonts it carries",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if c.notices == nil {
				return errors.New("this executable carries no licence texts")
			}
			return fs.WalkDir(c.notices, ".", func(path string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return err
				}
				text, err := fs.ReadFile(c.notices, path)
				if err != nil {
					return err
				}
				_, err = fmt.Fprintf(c.out, "==== %s ====\n\n%s\n", path, text)
				return err
			})
		},
	}
}
