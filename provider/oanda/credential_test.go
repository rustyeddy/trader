package oanda

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStaticCredential(t *testing.T) {
	tok, err := StaticCredential("abc").Token(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "abc", tok)
}
