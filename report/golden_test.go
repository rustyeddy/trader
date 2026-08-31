package report_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/report"
)

// assertGolden renders bt through renderer.Render and compares the
// output against testdata/<name>.golden, byte for byte — no trailing-
// newline normalization, so the renderer's own newline policy is part
// of what this test proves (issue #220 review, point 6).
//
// There is deliberately no "-update" flag or environment variable
// here: config/arch_test.go's TestDomainPackagesDoNotReadEnvOrFlags
// forbids any package outside config/cmd/test from importing "flag"
// or calling os.Getenv/LookupEnv/Environ, and report is not exempt. To
// regenerate a golden file, temporarily change the os.ReadFile call
// below to os.WriteFile(path, buf.Bytes(), 0o644), run the affected
// test once, then revert.
func assertGolden(t *testing.T, name string, renderer report.Renderer[report.BacktestReport], bt report.BacktestReport) {
	t.Helper()

	var buf bytes.Buffer
	require.NoError(t, renderer.Render(&buf, bt))

	path := filepath.Join("testdata", name+".golden")
	want, err := os.ReadFile(path)
	require.NoError(t, err, "golden file %s", path)
	require.Equal(t, string(want), buf.String())
}
