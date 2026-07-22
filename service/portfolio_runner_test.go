package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDrawdownCircuitBreaker_ZeroLimitAlwaysAllows(t *testing.T) {
	cb := &drawdownCircuitBreaker{limitPct: 0}
	assert.True(t, cb.allowOpen(context.Background()))
}
