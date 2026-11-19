// Package storetest points the store at a temporary directory for a test.
package storetest

import (
	"fmt"
	"os"
	"testing"

	"github.com/rustyeddy/trader/internal/store"
)

// Root makes a fresh temporary directory the store's root for the duration of
// the test, restores the previous root afterwards, and returns the directory.
// Tests never use the default root, which is the real archive.
func Root(t testing.TB) string {
	t.Helper()
	prev := store.Root()
	dir := t.TempDir()
	if err := store.SetRoot(dir); err != nil {
		t.Fatalf("storetest: %v", err)
	}
	t.Cleanup(func() { _ = store.SetRoot(prev) })
	return store.Root()
}

// Main runs a package's tests with a temporary directory as the store root, so
// that a test that forgets Root still never touches the real data root. Use it
// as: func TestMain(m *testing.M) { os.Exit(storetest.Main(m)) }.
func Main(m *testing.M) int {
	dir, err := os.MkdirTemp("", "store-test-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "storetest:", err)
		return 1
	}
	defer func() { _ = os.RemoveAll(dir) }()
	if err := store.SetRoot(dir); err != nil {
		fmt.Fprintln(os.Stderr, "storetest:", err)
		return 1
	}
	return m.Run()
}
