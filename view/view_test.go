package view

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRender_ExecutesTemplateAgainstArbitraryData(t *testing.T) {
	type point struct{ X, Y int }
	var buf bytes.Buffer
	require.NoError(t, Render(&buf, "({{.X}}, {{.Y}})", point{X: 3, Y: 4}))
	assert.Equal(t, "(3, 4)", buf.String())
}

func TestRender_InvalidTemplateErrors(t *testing.T) {
	var buf bytes.Buffer
	err := Render(&buf, "{{.Broken", nil)
	require.Error(t, err)
}
