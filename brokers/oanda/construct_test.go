package oanda

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── readTokenFile ────────────────────────────────────────────────────────────

func TestReadTokenFile_MissingFileReturnsEmpty(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	assert.Equal(t, "", readTokenFile())
}

func TestReadTokenFile_ReadsAndTrimsWhitespace(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	tokenDir := filepath.Join(dir, ".config", "oanda")
	require.NoError(t, os.MkdirAll(tokenDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(tokenDir, "pat.txt"), []byte("  mytoken\n"), 0o600))

	assert.Equal(t, "mytoken", readTokenFile())
}

func TestReadTokenFile_EmptyFileReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	tokenDir := filepath.Join(dir, ".config", "oanda")
	require.NoError(t, os.MkdirAll(tokenDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(tokenDir, "pat.txt"), []byte("   \n"), 0o600))

	assert.Equal(t, "", readTokenFile())
}

// ── ResolveToken ─────────────────────────────────────────────────────────────

func TestResolveToken_ExplicitWins(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	tokenDir := filepath.Join(dir, ".config", "oanda")
	require.NoError(t, os.MkdirAll(tokenDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(tokenDir, "pat.txt"), []byte("file-token"), 0o600))

	assert.Equal(t, "explicit-token", ResolveToken("explicit-token"))
}

func TestResolveToken_FallsBackToFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	tokenDir := filepath.Join(dir, ".config", "oanda")
	require.NoError(t, os.MkdirAll(tokenDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(tokenDir, "pat.txt"), []byte("file-token"), 0o600))

	assert.Equal(t, "file-token", ResolveToken(""))
}

func TestResolveToken_NoTokenAnywhereReturnsEmpty(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	assert.Equal(t, "", ResolveToken(""))
}

// ── NewClient ────────────────────────────────────────────────────────────────

func TestNewClient_NoTokenErrors(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	_, err := NewClient("practice", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no token")
}

func TestNewClient_InvalidEnvErrors(t *testing.T) {
	_, err := NewClient("nonsense", "tok")
	require.Error(t, err)
}

func TestNewClient_HappyPath(t *testing.T) {
	c, err := NewClient("practice", "tok")
	require.NoError(t, err)
	assert.Equal(t, "tok", c.Token)
	assert.Equal(t, "https://api-fxpractice.oanda.com", c.BaseURL)
}
