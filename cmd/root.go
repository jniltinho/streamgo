// Package cmd defines the streamgo command tree (Cobra): server, up, migrate, healthcheck, user,
// licenses and version. It is the composition root: it loads the
// configuration and wires the internal packages together.
//
// Portions adapted from MeetHub (MIT): cmd/root.go
package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/jniltinho/streamgo/internal/config"
)

// cli carries the global flags and I/O shared by every command.
type cli struct {
	cfgFile    string
	dataDir    string // --data-dir, applied when dataDirSet
	dataDirSet bool
	asJSON     bool
	in         io.Reader
	out        io.Writer
	errOut     io.Writer
	cfg        *config.Config
	notices    fs.FS                // the licence texts streamgo licenses prints
	override   func(*config.Config) // flags of the running command over the environment (up)
	geteuid    func() int           // nil: os.Geteuid
	executable string               // the own executable up reads its payload from; empty: os.Executable
}

// Execute runs the CLI with the process arguments and exits with its status; notices holds the
// licence texts embedded in the executable.
func Execute(notices fs.FS) {
	os.Exit(runCLI(&cli{in: os.Stdin, out: os.Stdout, errOut: os.Stderr, notices: notices}, os.Args[1:]))
}

// run executes the command line and returns the exit status (0 ok, 1 error).
func run(args []string, in io.Reader, out, errOut io.Writer) int {
	return runCLI(&cli{in: in, out: out, errOut: errOut}, args)
}

func runCLI(c *cli, args []string) int {
	root := newRootCmd(c)
	root.SetArgs(args)
	root.SetIn(c.in)
	root.SetOut(c.out)
	root.SetErr(c.errOut)
	if err := root.ExecuteContext(context.Background()); err != nil {
		_, _ = fmt.Fprintln(c.errOut, "error:", err)
		return 1
	}
	return 0
}

func newRootCmd(c *cli) *cobra.Command {
	root := &cobra.Command{
		Use:           "streamgo",
		Short:         "StreamGo — a self-hosted live-streaming studio",
		Long:          "StreamGo is a self-hosted live-streaming studio: server, database migrations and administration.",
		SilenceUsage:  true,
		SilenceErrors: true,
		// no configuration here: version, licenses and --help must work without one
		PersistentPreRun: func(cmd *cobra.Command, _ []string) {
			c.dataDirSet = cmd.Flags().Changed("data-dir")
		},
	}
	root.PersistentFlags().StringVar(&c.cfgFile, "config", "", "config file (default ./config.toml, then /etc/streamgo/config.toml)")
	root.PersistentFlags().StringVar(&c.dataDir, "data-dir", config.Default().Server.DataDir,
		"data directory: database, assets, streamgo.env (overrides server.data_dir)")
	root.PersistentFlags().BoolVar(&c.asJSON, "json", false, "print JSON output")
	root.AddCommand(
		newHealthcheckCmd(c),
		newLicensesCmd(c),
		newMigrateCmd(c),
		newServerCmd(c),
		newUpCmd(c),
		newUserCmd(c),
		newVersionCmd(c),
	)
	return root
}

// config loads and validates the configuration once, on first use, so commands that do not
// need it (version) never touch it.
func (c *cli) config() (*config.Config, error) {
	if c.cfg != nil {
		return c.cfg, nil
	}
	cfg, err := c.source().Load()
	if err != nil {
		return nil, err
	}
	c.cfg = cfg
	return cfg, nil
}

// source is where the configuration comes from: --config, the environment, and the flags given
// (--data-dir and those of the running command).
func (c *cli) source() config.Source {
	return config.Source{Path: c.cfgFile, Override: func(cfg *config.Config) {
		if c.dataDirSet {
			cfg.Server.DataDir = c.dataDir
		}
		if c.override != nil {
			c.override(cfg)
		}
	}}
}

// printJSON writes v as indented JSON.
func (c *cli) printJSON(v any) error {
	enc := json.NewEncoder(c.out)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// logger is the structured JSON logger on standard error.
func (c *cli) logger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(c.errOut, nil))
}
