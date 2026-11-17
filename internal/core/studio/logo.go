package studio

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/jniltinho/streamgo/internal/core"
	"github.com/jniltinho/streamgo/internal/core/broadcast"
	"github.com/jniltinho/streamgo/internal/core/stage"
)

// LogoURLPrefix is where logos are served (StreamGo's own origin, so the canvas is never
// tainted). Logos live in a directory of their own so they never shadow the web app's /assets/.
const LogoURLPrefix = "/assets/logos/"

// LogoTypes maps the logo file extensions to their content types.
var LogoTypes = map[string]string{".png": "image/png", ".jpg": "image/jpeg", ".webp": "image/webp"}

// logoName matches the names SetLogo writes, and nothing else.
var logoName = regexp.MustCompile(`^[0-9a-f]{32}\.(png|jpg|webp)$`)

// LogoPath returns the file of a logo name, or "" when name is not one.
func (s *Service) LogoPath(name string) string {
	if !logoName.MatchString(name) {
		return ""
	}
	return filepath.Join(s.AssetsDir, name)
}

// SetLogo stores an image the caller already checked (PNG, JPEG or WebP within the limits,
// ext one of LogoTypes) as the broadcast's logo. The new file is written and recorded before
// the old one is deleted, so a failure keeps the previous logo.
func (s *Service) SetLogo(ctx context.Context, userID, broadcastID string, data []byte, ext string) (stage.State, error) {
	if _, ok := LogoTypes[ext]; !ok {
		return stage.State{}, core.Invalid("file", "The logo must be a PNG, JPEG or WebP image.")
	}
	if _, err := s.owned(ctx, userID, broadcastID); err != nil {
		return stage.State{}, err
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return stage.State{}, err
	}
	name := hex.EncodeToString(b) + ext
	if err := writeFile(s.AssetsDir, name, data); err != nil {
		return stage.State{}, err
	}
	return s.changeLogo(ctx, broadcastID, name)
}

// DeleteLogo removes the broadcast's logo.
func (s *Service) DeleteLogo(ctx context.Context, userID, broadcastID string) (stage.State, error) {
	if _, err := s.owned(ctx, userID, broadcastID); err != nil {
		return stage.State{}, err
	}
	return s.changeLogo(ctx, broadcastID, "")
}

// changeLogo records name ("" for none) and deletes the previous file once the change is
// stored, or the new one when it is not.
func (s *Service) changeLogo(ctx context.Context, broadcastID, name string) (stage.State, error) {
	var old string
	st, err := s.Stage.Change(ctx, broadcastID, func(ctx context.Context, b *broadcast.Broadcast, st *stage.State) error {
		old = b.Logo
		if err := s.Broadcasts.SetLogo(ctx, b.ID, name); err != nil {
			return fmt.Errorf("record the logo: %w", err)
		}
		st.Logo = nil
		if name != "" {
			url := LogoURLPrefix + name
			st.Logo = &url
		}
		return nil
	})
	if err != nil {
		s.removeLogoFile(name)
		return stage.State{}, err
	}
	if old != name {
		s.removeLogoFile(old)
	}
	return st, nil
}

func (s *Service) removeLogoFile(name string) {
	p := s.LogoPath(name)
	if p == "" {
		return
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		s.logger().Warn("delete a logo file", "error", err)
	}
}

// writeFile writes data to dir/name through a temporary file, so the name never holds a
// partial image.
func writeFile(dir, name string, data []byte) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create the assets directory: %w", err)
	}
	f, err := os.CreateTemp(dir, ".upload-*")
	if err != nil {
		return fmt.Errorf("store the logo: %w", err)
	}
	_, werr := f.Write(data)
	cerr := f.Close()
	if werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Chmod(f.Name(), 0o640)
	}
	if werr == nil {
		werr = os.Rename(f.Name(), filepath.Join(dir, name))
	}
	if werr != nil {
		_ = os.Remove(f.Name())
		return fmt.Errorf("store the logo: %w", werr)
	}
	return nil
}
