package oanda

import (
	"context"
	"sync"
)

// partitionLocks serializes writers of one partition file within this process.
// Atomic replace keeps a file whole but cannot stop two overlapping
// read-merge-replace cycles from losing the newer one's records, so they hold
// the partition's lock for the whole cycle (ADR-089). The lock is process-local;
// it does not coordinate separate processes.
var partitionLocks = struct {
	mu sync.Mutex
	m  map[string]*partitionLock
}{m: map[string]*partitionLock{}}

// partitionLock is a one-slot semaphore, so acquiring it can respect ctx, with
// a count of holders and waiters so the registry does not grow without bound.
type partitionLock struct {
	ch   chan struct{}
	refs int
}

// lockPartition blocks until the lock for path is held or ctx is done, and
// returns the function that releases it. It never calls ctx.Err, only waits on
// ctx.Done.
func lockPartition(ctx context.Context, path string) (func(), error) {
	partitionLocks.mu.Lock()
	l := partitionLocks.m[path]
	if l == nil {
		l = &partitionLock{ch: make(chan struct{}, 1)}
		partitionLocks.m[path] = l
	}
	l.refs++
	partitionLocks.mu.Unlock()

	release := func() {
		partitionLocks.mu.Lock()
		l.refs--
		if l.refs == 0 {
			delete(partitionLocks.m, path)
		}
		partitionLocks.mu.Unlock()
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
