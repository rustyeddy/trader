package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileValuesNeitherPathNorContentSet(t *testing.T) {
	values, _, err := fileValues(Options{})
	require.NoError(t, err)
	assert.Nil(t, values)
}

func TestFileValuesFromContent(t *testing.T) {
	values, _, err := fileValues(Options{FileContent: []byte(`
name: trader
server:
  host: localhost
  port: 8080
`)})
	require.NoError(t, err)
	assert.Equal(t, "trader", values["name"])
	assert.Equal(t, "localhost", values["server.host"])
	assert.Equal(t, "8080", values["server.port"])
}

func TestFileValuesFromPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("port: 9090\n"), 0o644))

	values, _, err := fileValues(Options{FilePath: path})
	require.NoError(t, err)
	assert.Equal(t, "9090", values["port"])
}

func TestFileValuesContentTakesPrecedenceOverPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("port: 9090\n"), 0o644))

	values, _, err := fileValues(Options{FilePath: path, FileContent: []byte("port: 1111\n")})
	require.NoError(t, err)
	assert.Equal(t, "1111", values["port"])
}

func TestFileValuesMissingPathErrors(t *testing.T) {
	_, _, err := fileValues(Options{FilePath: "/does/not/exist.yaml"})
	require.Error(t, err)
}

func TestFileValuesMalformedYAMLErrors(t *testing.T) {
	_, _, err := fileValues(Options{FileContent: []byte("not: [valid: yaml")})
	require.Error(t, err)
}

func TestFileValuesTopLevelScalarErrors(t *testing.T) {
	_, _, err := fileValues(Options{FileContent: []byte("just a string\n")})
	require.Error(t, err)
}

func TestFileValuesEmptyContent(t *testing.T) {
	values, _, err := fileValues(Options{FileContent: []byte("")})
	require.NoError(t, err)
	assert.Empty(t, values)
}

func TestFileValuesNullSkipsKey(t *testing.T) {
	values, _, err := fileValues(Options{FileContent: []byte("port: null\nname: trader\n")})
	require.NoError(t, err)
	_, ok := values["port"]
	assert.False(t, ok, "an explicit null must not participate in the file source")
	assert.Equal(t, "trader", values["name"])
}

func TestFileValuesKeysAreLowercased(t *testing.T) {
	values, _, err := fileValues(Options{FileContent: []byte("Server:\n  Port: 8080\n")})
	require.NoError(t, err)
	assert.Equal(t, "8080", values["server.port"])
}

func TestFileValuesBoolAndFloat(t *testing.T) {
	values, _, err := fileValues(Options{FileContent: []byte("enabled: true\nratio: 1.5\n")})
	require.NoError(t, err)
	assert.Equal(t, "true", values["enabled"])
	assert.Equal(t, "1.5", values["ratio"])
}

// Path presence covers every key, whatever its value's node kind, while
// the decoded value map keeps its scalar-only, null-is-absent semantics.
func TestFileValuesPathsCoverEveryNodeKind(t *testing.T) {
	values, paths, err := fileValues(Options{FileContent: []byte("a: null\nb: [1]\nc:\n  d: 1\n  e: {f: 2}\n")})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"c.d": "1", "c.e.f": "2"}, values)
	for _, p := range []string{"a", "b", "c", "c.d", "c.e", "c.e.f"} {
		assert.True(t, paths[p], p)
	}
}
