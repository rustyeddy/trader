package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveVersion(t *testing.T) {
	tests := []struct {
		name, describe, module, want string
	}{
		{"the git describe of a make build wins", "v0.5.0", "v0.4.0", "v0.5.0"},
		{"past a tag and dirty, as describe says", "v0.5.0-12-gabc1234-dirty", "", "v0.5.0-12-gabc1234-dirty"},
		{"go install module@tag reports the tag", "", "v0.5.0", "v0.5.0"},
		{"go's devel placeholder is not a version", "", "(devel)", "devel"},
		{"nothing known", "", "", "devel"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, resolveVersion(tt.describe, tt.module))
		})
	}
}

func TestVersionCmd_PrintsTheVersion(t *testing.T) {
	silenceLogs(t)
	prev := gitDescribe
	gitDescribe = "v9.9.9"
	t.Cleanup(func() { gitDescribe = prev })

	var out bytes.Buffer
	a := newApp()
	a.cmd.SetOut(&out)
	a.cmd.SetArgs([]string{"version"})

	require.NoError(t, a.execute())

	assert.Equal(t, "trader v9.9.9\n", out.String())
}

func TestRootCmd_VersionFlagPrintsTheSameVersion(t *testing.T) {
	silenceLogs(t)
	prev := gitDescribe
	gitDescribe = "v9.9.9"
	t.Cleanup(func() { gitDescribe = prev })

	var out bytes.Buffer
	a := newApp()
	a.cmd.SetOut(&out)
	a.cmd.SetArgs([]string{"--version"})

	require.NoError(t, a.execute())

	assert.Equal(t, "trader v9.9.9\n", out.String())
}
