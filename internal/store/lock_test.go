package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLockPartition_WaiterHonorsCancellation(t *testing.T) {
	unlock, err := lock(context.Background(), "p")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := lock(ctx, "p")
		done <- err
	}()

	cancel()

	assert.ErrorIs(t, <-done, context.Canceled)
	unlock()
	pathLocks.mu.Lock()
	defer pathLocks.mu.Unlock()
	assert.Empty(t, pathLocks.m)
}

func TestLockPartition_DifferentPartitionsDoNotBlockEachOther(t *testing.T) {
	a, err := lock(context.Background(), "a")
	require.NoError(t, err)
	defer a()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	b, err := lock(ctx, "b")

	require.NoError(t, err)
	b()
}
