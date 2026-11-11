package marketdata

import "context"

// WriteLock serializes the operations that write a provider's raw and
// canonical data: Sync, Build, and ImportStooqArchive each hold it for
// their whole call. Sync extends a raw partition by reading it, fetching,
// merging, and replacing it; atomic file replacement keeps the file whole
// but cannot stop two overlapping read-modify-write calls from losing the
// newer one's records, so they must not overlap.
//
// Managers that share storage must share one WriteLock (Config.WriteLock).
// A long-running transport building a Manager per request (trader-mcp's
// marketdatacfg.Factory) holds one per provider for exactly this reason.
// Acquiring it respects context cancellation, so a request waiting its
// turn can still be canceled.
type WriteLock struct {
	ch chan struct{}
}

// NewWriteLock returns an unheld WriteLock.
func NewWriteLock() *WriteLock {
	return &WriteLock{ch: make(chan struct{}, 1)}
}

// acquire blocks until the lock is held or ctx is done. A nil lock is
// always free.
func (l *WriteLock) acquire(ctx context.Context) error {
	if l == nil {
		return nil
	}
	select {
	case l.ch <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (l *WriteLock) release() {
	if l != nil {
		<-l.ch
	}
}
