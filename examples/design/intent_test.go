package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAction_String(t *testing.T) {
	tests := map[action]string{
		hold:       "hold",
		open:       "open",
		add:        "add",
		close:      "close",
		reduce:     "reduce",
		action(99): "unknown",
	}
	for a, want := range tests {
		assert.Equal(t, want, a.String())
	}
}

func TestDirection_String(t *testing.T) {
	tests := map[direction]string{
		noDirection:   "none",
		long:          "long",
		short:         "short",
		direction(99): "none",
	}
	for d, want := range tests {
		assert.Equal(t, want, d.String())
	}
}
