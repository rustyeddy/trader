package docs

import (
	"bytes"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── genMarkdownAll ────────────────────────────────────────────────────────────

func TestGenMarkdownAll_WritesRootAndSubcommand(t *testing.T) {
	root := &cobra.Command{Use: "myapp", Short: "My app"}
	sub := &cobra.Command{Use: "sub", Short: "A subcommand"}
	root.AddCommand(sub)

	var buf bytes.Buffer
	require.NoError(t, genMarkdownAll(root, &buf))

	out := buf.String()
	assert.Contains(t, out, "myapp")
	assert.Contains(t, out, "sub")
}

func TestGenMarkdownAll_SkipsHiddenCommands(t *testing.T) {
	root := &cobra.Command{Use: "myapp", Short: "My app"}
	hidden := &cobra.Command{Use: "secret", Short: "hidden cmd", Hidden: true}
	root.AddCommand(hidden)

	var buf bytes.Buffer
	require.NoError(t, genMarkdownAll(root, &buf))

	assert.NotContains(t, buf.String(), "secret")
}

func TestGenMarkdownAll_SubcommandsInAlphaOrder(t *testing.T) {
	root := &cobra.Command{Use: "myapp", Short: "My app"}
	root.AddCommand(&cobra.Command{Use: "zebra", Short: "z"})
	root.AddCommand(&cobra.Command{Use: "alpha", Short: "a"})
	root.AddCommand(&cobra.Command{Use: "mango", Short: "m"})

	var buf bytes.Buffer
	require.NoError(t, genMarkdownAll(root, &buf))

	out := buf.String()
	alphaIdx := bytes.Index([]byte(out), []byte("alpha"))
	mangoIdx := bytes.Index([]byte(out), []byte("mango"))
	zebraIdx := bytes.Index([]byte(out), []byte("zebra"))

	assert.Less(t, alphaIdx, mangoIdx, "alpha should appear before mango")
	assert.Less(t, mangoIdx, zebraIdx, "mango should appear before zebra")
}

// ── docs command error paths ──────────────────────────────────────────────────

func TestDocsCmd_UnknownFormatReturnsError(t *testing.T) {
	cmd := New(nil)
	cmd.SetArgs([]string{"--single=false", "--format", "pdf"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pdf")
}

func TestDocsCmd_SingleWithNonMarkdownReturnsError(t *testing.T) {
	cmd := New(nil)
	cmd.SetArgs([]string{"--single=true", "--format", "man"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--single")
}
