package log_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tlog "github.com/rustyeddy/trader/log"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// -------------------------------------------------------------------------
// Setup / level tests
// -------------------------------------------------------------------------

func TestSetup_DefaultInfo(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "default_info.log")

	// At level "info", debug messages must be suppressed.
	require.NoError(t, tlog.Setup(tlog.Config{Level: "info", File: logPath}))

	tlog.Debug("should not appear")
	tlog.Info("should appear")

	data, err := os.ReadFile(logPath)
	require.NoError(t, err)
	content := string(data)

	assert.NotContains(t, content, "should not appear",
		"debug message must be suppressed at info level")
	assert.Contains(t, content, "should appear",
		"info message must be emitted at info level")
}

func TestSetup_LevelDebug(t *testing.T) {
	require.NoError(t, tlog.Setup(tlog.Config{Level: "debug"}))
}

func TestSetup_LevelWarn(t *testing.T) {
	require.NoError(t, tlog.Setup(tlog.Config{Level: "warn"}))
}

func TestSetup_LevelError(t *testing.T) {
	require.NoError(t, tlog.Setup(tlog.Config{Level: "error"}))
}

func TestSetup_LevelCaseInsensitive(t *testing.T) {
	for _, lvl := range []string{"DEBUG", "Info", "WARN", "ERROR", "warning"} {
		require.NoError(t, tlog.Setup(tlog.Config{Level: lvl}), "level: %s", lvl)
	}
}

// -------------------------------------------------------------------------
// File output
// -------------------------------------------------------------------------

func TestSetup_FileOutput(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "test.log")

	require.NoError(t, tlog.Setup(tlog.Config{
		Level:  "info",
		Format: "text",
		File:   logPath,
	}))

	tlog.Info("hello from test", "key", "value")

	// Allow the handler to flush; it writes synchronously so no sleep needed.
	data, err := os.ReadFile(logPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "hello from test")
	assert.Contains(t, string(data), "key=value")
}

func TestSetup_FileOutput_JSON(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "test.json.log")

	require.NoError(t, tlog.Setup(tlog.Config{
		Level:  "debug",
		Format: "json",
		File:   logPath,
	}))

	tlog.Info("json test", "n", 42)

	data, err := os.ReadFile(logPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"msg":"json test"`)
	assert.Contains(t, string(data), `"n":42`)
}

func TestSetup_InvalidFile(t *testing.T) {
	err := tlog.Setup(tlog.Config{
		File: "/nonexistent-dir/bad/path.log",
	})
	assert.Error(t, err)
}

// -------------------------------------------------------------------------
// Module loggers
// -------------------------------------------------------------------------

func TestModule_ReturnsSameLogger(t *testing.T) {
	require.NoError(t, tlog.Setup(tlog.Config{}))

	a := tlog.Module("alpha")
	b := tlog.Module("alpha")
	assert.Same(t, a, b, "Module() should return the same instance for the same name")
}

func TestModule_DifferentNames(t *testing.T) {
	require.NoError(t, tlog.Setup(tlog.Config{}))

	x := tlog.Module("x")
	y := tlog.Module("y")
	assert.NotSame(t, x, y)
}

func TestModuleLoggers_Wired(t *testing.T) {
	require.NoError(t, tlog.Setup(tlog.Config{}))

	// Pre-wired module variables must be non-nil after Setup.
	assert.NotNil(t, tlog.Data)
	assert.NotNil(t, tlog.Backtest)
	assert.NotNil(t, tlog.Indicator)
	assert.NotNil(t, tlog.Replay)
}

// -------------------------------------------------------------------------
// Module attribute propagation
// -------------------------------------------------------------------------

func TestModule_ContainsModuleAttr(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "module.log")

	require.NoError(t, tlog.Setup(tlog.Config{
		Level: "debug",
		File:  logPath,
	}))

	tlog.Module("mymod").Info("module message")

	data, err := os.ReadFile(logPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "module=mymod",
		"log record should carry the module attribute")
	assert.Contains(t, string(data), "module message")
}

func TestPrewiredModuleAttr_Data(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "data.log")

	require.NoError(t, tlog.Setup(tlog.Config{Level: "debug", File: logPath}))
	tlog.Data.Info("data event")

	data, err := os.ReadFile(logPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "module=data")
}

// -------------------------------------------------------------------------
// Multiple Setup calls (re-initialisation)
// -------------------------------------------------------------------------

func TestSetup_ReInitialisation(t *testing.T) {
	dir := t.TempDir()
	logPath1 := filepath.Join(dir, "log1.log")
	logPath2 := filepath.Join(dir, "log2.log")

	require.NoError(t, tlog.Setup(tlog.Config{Level: "info", File: logPath1}))
	tlog.Info("first setup")

	require.NoError(t, tlog.Setup(tlog.Config{Level: "debug", File: logPath2}))
	tlog.Debug("second setup")

	data2, err := os.ReadFile(logPath2)
	require.NoError(t, err)
	assert.Contains(t, string(data2), "second setup")
}

// -------------------------------------------------------------------------
// parseLevel edge cases (via Setup)
// -------------------------------------------------------------------------

func TestSetup_UnknownLevelDefaultsToInfo(t *testing.T) {
	// "trace" is not a recognised level; should fall back to info silently.
	require.NoError(t, tlog.Setup(tlog.Config{Level: "trace"}))
}

// -------------------------------------------------------------------------
// Package-level convenience functions smoke test
// -------------------------------------------------------------------------

func TestPackageLevelFunctions(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "pkgfns.log")
	require.NoError(t, tlog.Setup(tlog.Config{Level: "debug", File: logPath}))

	tlog.Debug("dbg", "k", 1)
	tlog.Info("inf", "k", 2)
	tlog.Warn("wrn", "k", 3)
	tlog.Error("err", "k", 4)

	data, err := os.ReadFile(logPath)
	require.NoError(t, err)
	content := string(data)

	for _, fragment := range []string{"dbg", "inf", "wrn", "err"} {
		assert.True(t, strings.Contains(content, fragment),
			"expected %q in log output", fragment)
	}
}
