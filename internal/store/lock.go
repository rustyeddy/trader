package store

import (
	"context"
	"sync"
)

// pathLocks serializes writers of one file within this process. Atomic replace
// keeps a file whole but cannot stop two overlapping read-merge-replace cycles
// from losing the newer one's data, so a cycle holds the path's lock throughout.
// The lock is process-local; it does not coordinate separate processes.
var pathLocks = struct {
	mu sync.Mutex
	m  map[string]*pathLock
}{m: map[string]*pathLock{}}

// pathLock is a one-slot semaphore, so acquiring it can respect ctx, with a
// count of holders and waiters so the registry does not grow without bound.
type pathLock struct {
	ch   chan struct{}
	refs int
}

// lock blocks until the lock for the absolute path is held or ctx is done, and
// returns the function that releases it. It only waits on ctx.Done.
func lock(ctx context.Context, abs string) (func(), error) {
	pathLocks.mu.Lock()
	l := pathLocks.m[abs]
	if l == nil {
		l = &pathLock{ch: make(chan struct{}, 1)}
		pathLocks.m[abs] = l
	}
	l.refs++
	pathLocks.mu.Unlock()

	release := func() {
		pathLocks.mu.Lock()
		l.refs--
		if l.refs == 0 {
			delete(pathLocks.m, abs)
		}
		pathLocks.mu.Unlock()
	}

	select {
	case l.ch <- struct{}{}:
		return func() {
			<-l.ch
			release()
		}, nil
	case <-ctx.Done():
		release()
		return nil, ctx.Err()
	}
}
