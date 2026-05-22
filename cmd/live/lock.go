package live

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// acquireAccountLock acquires an exclusive advisory flock on a per-account
// lock file in the OS temp directory. It returns the open file, a release
// function, and an error if another process already holds the lock.
//
// The OS releases the lock automatically when the process exits — including
// on crash or SIGKILL — so stale lock files never block a restart.
func acquireAccountLock(accountID string) (*os.File, func(), error) {
	path := filepath.Join(os.TempDir(), "trader-"+accountID+".lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, nil, fmt.Errorf("open lock file: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, nil, fmt.Errorf("another bot is already running for account %s (lock: %s)", accountID, path)
		}
		return nil, nil, fmt.Errorf("acquire lock: %w", err)
	}
	release := func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}
	return f, release, nil
}
