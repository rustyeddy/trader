//go:build blackbox

package blackbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

var traderBin string

func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "trader-blackbox-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tmp)

	traderBin = filepath.Join(tmp, "trader")

	// Build the binary once for all tests.
	cmd := exec.Command("go", "build", "-o", traderBin, "./cmd/trader")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		panic(err)
	}

	os.Exit(m.Run())
}

func run(t *testing.T, args ...string) (stdout string, stderr string) {
	t.Helper()

	cmd := exec.Command(traderBin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// CombinedOutput merges stdout/stderr; still useful in failures.
		t.Fatalf("command failed: %v\nargs: %v\noutput:\n%s", err, args, string(out))
	}
	return string(out), ""
}
