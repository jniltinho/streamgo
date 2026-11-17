//go:build unix

package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// InstanceLockFile is the lock file under data_dir that keeps one server per deployment (D12).
const InstanceLockFile = "streamgo.lock"

// ErrInstanceRunning means another process holds the instance lock.
var ErrInstanceRunning = errors.New("another StreamGo instance is running with this data_dir")

// Instance is the held instance lock. The kernel releases it when the process ends, however it
// ends, so a killed server leaves nothing to clean up.
type Instance struct{ f *os.File }

// LockInstance takes the exclusive lock on <dataDir>/streamgo.lock without waiting.
func LockInstance(dataDir string) (*Instance, error) {
	path := filepath.Join(dataDir, InstanceLockFile)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // the operator's own data_dir
	if err != nil {
		return nil, fmt.Errorf("instance lock: %w", err)
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil { //nolint:gosec // a file descriptor fits an int
		_ = f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, fmt.Errorf("%w (%s is locked)", ErrInstanceRunning, path)
		}
		return nil, fmt.Errorf("instance lock %s: %w", path, err)
	}
	return &Instance{f: f}, nil
}

// Close releases the lock. It unlocks before closing: a child forked meanwhile holds a copy of the
// descriptor until it execs, and closing ours alone would leave the lock held for that moment.
func (i *Instance) Close() error {
	err := unix.Flock(int(i.f.Fd()), unix.LOCK_UN) //nolint:gosec // a file descriptor fits an int
	return errors.Join(err, i.f.Close())
}
