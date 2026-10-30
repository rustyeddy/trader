package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAccount_MarginAvailable(t *testing.T) {
	tests := []struct {
		name    string
		balance money
		want    money
	}{
		{"positive", 10000, 10000},
		{"zero", 0, 0},
		{"negative", -50, -50},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, newAccount("a", tt.balance).marginAvailable())
		})
	}
}
