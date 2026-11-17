package cmd

import (
	"crypto/tls"
	"fmt"
	"log/slog"
	"time"

	"github.com/jniltinho/streamgo/internal/config"
	"github.com/jniltinho/streamgo/internal/tlsutil"
)

// serverTLS is what the server speaks HTTPS with: the configured pair, or the self-signed one under
// <data_dir>/tls (generated or renewed first). Nil for plain HTTP.
func serverTLS(cfg *config.Config, logger *slog.Logger) (*tls.Config, error) {
	cert, key := cfg.TLSFiles()
	if cert == "" {
		return nil, nil
	}
	if cfg.Server.CertFile == "" { // self-signed
		created, err := tlsutil.EnsureSelfSigned(cert, key, []string{cfg.PublicHost()}, time.Now())
		if err != nil {
			return nil, fmt.Errorf("self-signed certificate under %s: %w", cert, err)
		}
		if created {
			logger.Info("generated a self-signed certificate", "file", cert, "host", cfg.PublicHost())
		}
	}
	tc, _, err := tlsutil.Load(cert, key)
	return tc, err
}
