package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOptionsEnvironIsSet(t *testing.T) {
	var zero Options
	assert.False(t, zero.environIsSet(), "nil Environ means 'read the real environment'")

	empty := Options{Environ: []string{}}
	assert.True(t, empty.environIsSet(), "an explicit empty slice disables the environment source")

	set := Options{Environ: []string{"FOO=bar"}}
	assert.True(t, set.environIsSet())
}
