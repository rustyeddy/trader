package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestToOandaGranularity(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"D1", "D"},
		{"d1", "D"},
		{"D", "D"},
		{"d", "D"},
		{"H1", "H1"},
		{"h1", "H1"},
		{"M1", "M1"},
		{"m1", "M1"},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, toOandaGranularity(tc.in), "input %q", tc.in)
	}
}

func TestParseTraderTimeframe(t *testing.T) {
	_, err := parseTraderTimeframe("D1")
	assert.NoError(t, err)

	_, err = parseTraderTimeframe("D")
	assert.NoError(t, err)

	_, err = parseTraderTimeframe("bogus")
	assert.Error(t, err)
}
