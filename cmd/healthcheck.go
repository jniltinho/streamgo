package cmd

// Portions adapted from MeetHub (MIT): cmd/healthcheck.go

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/spf13/cobra"
)

// healthcheckTimeout bounds the whole probe.
const healthcheckTimeout = 5 * time.Second

func newHealthcheckCmd(c *cli) *cobra.Command {
	return &cobra.Command{
		Use:   "healthcheck",
		Short: "Probe the local server's readiness endpoint (for container health checks)",
		Long: "Requests /readyz on the address the server listens on (server.addr; a wildcard host becomes\n" +
			"127.0.0.1) and exits 0 when it answers 200 within 5 seconds, 1 otherwise. Proxy settings are ignored.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := c.config()
			if err != nil {
				return err
			}
			u, err := readyURL(cfg.Server.Addr)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), healthcheckTimeout)
			defer cancel()
			if err := probe(ctx, u); err != nil {
				return err
			}
			_, err = fmt.Fprintln(c.out, "ready")
			return err
		},
	}
}

// readyURL turns a listen address into the local readiness URL.
func readyURL(addr string) (string, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("server.addr %q: %w", addr, err)
	}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "127.0.0.1"
		if ip != nil && ip.To4() == nil {
			host = "::1"
		}
	}
	return "http://" + net.JoinHostPort(host, port) + "/readyz", nil
}

func probe(ctx context.Context, u string) error {
	client := &http.Client{Transport: &http.Transport{Proxy: nil}} // ignore HTTP(S)_PROXY
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, http.NoBody)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("readiness probe: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return errors.New("readiness probe: status " + resp.Status)
	}
	return nil
}
