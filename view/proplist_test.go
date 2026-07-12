package view

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPropertyList_AddAndRender(t *testing.T) {
	p := NewPropertyList()
	p.Add("Status", "running")
	p.Add("Strategy", "emacross")

	var buf bytes.Buffer
	require.NoError(t, p.Render(&buf))

	lines := p.Lines()
	assert.Equal(t, []string{"Status    running", "Strategy  emacross"}, lines)
	assert.Equal(t, "Status    running\nStrategy  emacross\n", buf.String())
}

func TestPropertyList_AddIfSkipsFalseCondition(t *testing.T) {
	p := NewPropertyList()
	p.AddIf(true, "Uptime", "3h")
	p.AddIf(false, "Error", "should not appear")

	lines := p.Lines()
	assert.Len(t, lines, 1)
	assert.Contains(t, lines[0], "Uptime")
}

func TestPropertyList_Empty(t *testing.T) {
	p := NewPropertyList()
	assert.Empty(t, p.Lines())

	var buf bytes.Buffer
	require.NoError(t, p.Render(&buf))
	assert.Empty(t, buf.String())
}
