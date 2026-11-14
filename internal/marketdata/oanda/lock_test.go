package oanda

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLockPartition_WaiterHonorsCancellation(t *testing.T) {
	unlock, err := lockPartition(context.Background(), "p")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := lockPartition(ctx, "p")
		done <- err
	}()

	cancel()

	assert.ErrorIs(t, <-done, context.Canceled)
	unlock()
	partitionLocks.mu.Lock()
	defer partitionLocks.mu.Unlock()
	assert.Empty(t, partitionLocks.m)
}

func TestLockPartition_DifferentPartitionsDoNotBlockEachOther(t *testing.T) {
	a, err := lockPartition(context.Background(), "a")
	require.NoError(t, err)
	defer a()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	b, err := lockPartition(ctx, "b")

	require.NoError(t, err)
	b()
}
