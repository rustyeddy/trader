package sdk

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHostClock_NowReflectsMostRecentSet(t *testing.T) {
	start := time.Date(2024, time.January, 2, 15, 0, 0, 0, time.UTC)
	c := newHostClock(start)
	require.True(t, c.Now().Equal(start))

	next := start.Add(time.Hour)
	c.set(next)
	require.True(t, c.Now().Equal(next))
}
