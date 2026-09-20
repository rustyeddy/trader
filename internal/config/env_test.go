package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEnvValues(t *testing.T) {
	got := envValues([]string{"FOO=bar", "PORT=8080", "EMPTY="})
	assert.Equal(t, "bar", got["FOO"])
	assert.Equal(t, "8080", got["PORT"])
	assert.Equal(t, "", got["EMPTY"])
	_, ok := got["MISSING"]
	assert.False(t, ok)
}

func TestEnvValuesSkipsMalformedEntries(t *testing.T) {
	got := envValues([]string{"NOVALUE", "FOO=bar"})
	assert.Len(t, got, 1)
	assert.Equal(t, "bar", got["FOO"])
}

func TestEnvValuesEmpty(t *testing.T) {
	got := envValues(nil)
	assert.Empty(t, got)
}
