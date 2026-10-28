package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBarsYieldsAllInOrder(t *testing.T) {
	md := &marketData{data: []ohlc{{c: 1}, {c: 2}, {c: 3}}}
	var got []price
	for b, err := range md.bars(context.Background()) {
		require.NoError(t, err)
		got = append(got, b.c)
	}
	require.Equal(t, []price{1, 2, 3}, got)
}

func TestBarsEarlyBreak(t *testing.T) {
	md := &marketData{data: []ohlc{{c: 1}, {c: 2}, {c: 3}}}
	n := 0
	for range md.bars(context.Background()) {
		n++
		break
	}
	require.Equal(t, 1, n)
}

func TestBarsContextCancelled(t *testing.T) {
	md := &marketData{data: []ohlc{{c: 1}, {c: 2}, {c: 3}}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var bars int
	var gotErr error
	for _, err := range md.bars(ctx) {
		if err != nil {
			gotErr = err
			break
		}
		bars++
		cancel() // cancel after the first bar
	}
	require.Equal(t, 1, bars)
	require.ErrorIs(t, gotErr, context.Canceled)
}

func TestBarsEmpty(t *testing.T) {
	md := &marketData{}
	for range md.bars(context.Background()) {
		t.Fatal("no bars expected")
	}
}
