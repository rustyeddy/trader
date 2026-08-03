package numericstudy

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The study machinery is shared by numericstudy and quantitystudy, so its
// failure modes are worth pinning: a mistyped document path, a marker typo, or
// a read-only checkout should produce a clear error rather than a silent
// no-op that lets the documents drift unnoticed.

// fixedFragments returns a Syncer whose fragments are static, so these tests
// exercise the machinery rather than the real study data.
func fixedFragments(ns string, docs []Doc) Syncer {
	return Syncer{
		Namespace: ns,
		Docs:      docs,
		Fragments: func(Format) map[string]string {
			return map[string]string{
				"demo": "| A | B |\n|---+---|\n| 1 | 2 |\n",
			}
		},
	}
}

func writeTemp(t *testing.T, name, content string) (root, rel string) {
	t.Helper()

	root = t.TempDir()
	rel = name
	require.NoError(t, os.WriteFile(filepath.Join(root, rel), []byte(content), 0o644))

	return root, rel
}

func TestSyncDocMissingFile(t *testing.T) {
	s := fixedFragments("demo", []Doc{{Path: "nope.org", Format: FormatOrg}})

	_, err := s.SyncDoc(t.TempDir(), s.Docs[0])
	assert.ErrorIs(t, err, os.ErrNotExist,
		"a mistyped document path must fail loudly, not silently skip")
}

func TestSyncPropagatesReadError(t *testing.T) {
	s := fixedFragments("demo", []Doc{{Path: "nope.org", Format: FormatOrg}})

	_, err := s.Sync(t.TempDir(), false)
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestUsedFragmentsMissingFile(t *testing.T) {
	s := fixedFragments("demo", []Doc{{Path: "nope.md", Format: FormatMarkdown}})

	_, err := s.UsedFragments(t.TempDir())
	assert.ErrorIs(t, err, os.ErrNotExist)
}

// TestSyncDocUnknownFragment covers a marker naming something the study does
// not generate — usually a typo.  The region must be left exactly as-is rather
// than blanked, so a mistake never destroys checked-in content.
func TestSyncDocUnknownFragment(t *testing.T) {
	const body = "before\n" +
		"# BEGIN demo:nosuch\nkeep me\n# END demo:nosuch\n" +
		"after\n"

	root, rel := writeTemp(t, "doc.org", body)
	s := fixedFragments("demo", []Doc{{Path: rel, Format: FormatOrg}})

	res, err := s.SyncDoc(root, s.Docs[0])
	require.NoError(t, err)

	assert.Equal(t, []string{"nosuch"}, res.Unknown)
	assert.Empty(t, res.Seen)
	assert.False(t, res.Changed)
	assert.Contains(t, res.Content, "keep me", "unknown regions must be preserved")
}

// TestSyncDocUnbalancedMarkers covers a BEGIN/END name mismatch, which is easy
// to introduce when copying a region.  Like an unknown fragment, the content
// is preserved and the problem reported.
func TestSyncDocUnbalancedMarkers(t *testing.T) {
	const body = "# BEGIN demo:demo\nkeep me\n# END demo:other\n"

	root, rel := writeTemp(t, "doc.org", body)
	s := fixedFragments("demo", []Doc{{Path: rel, Format: FormatOrg}})

	res, err := s.SyncDoc(root, s.Docs[0])
	require.NoError(t, err)

	assert.Equal(t, []string{"demo/other"}, res.Unbalanced)
	assert.Empty(t, res.Seen)
	assert.Contains(t, res.Content, "keep me")
}

// TestSyncWritesOnlyWhenChanged pins the write path, including that a document
// already in sync is not rewritten — so regenerating does not churn mtimes or
// produce empty diffs.
func TestSyncWritesOnlyWhenChanged(t *testing.T) {
	const stale = "# BEGIN demo:demo\nold\n# END demo:demo\n"

	root, rel := writeTemp(t, "doc.org", stale)
	s := fixedFragments("demo", []Doc{{Path: rel, Format: FormatOrg}})

	res, err := s.Sync(root, true)
	require.NoError(t, err)
	require.Len(t, res, 1)
	assert.True(t, res[0].Changed)
	assert.Equal(t, []string{"demo"}, res[0].Stale)

	onDisk, err := os.ReadFile(filepath.Join(root, rel))
	require.NoError(t, err)
	assert.Contains(t, string(onDisk), "| 1 | 2 |")

	// Second pass: already current, nothing stale, nothing rewritten.
	again, err := s.Sync(root, true)
	require.NoError(t, err)
	assert.False(t, again[0].Changed)
	assert.Empty(t, again[0].Stale)
	assert.Equal(t, []string{"demo"}, again[0].Seen)
}

// TestSyncWriteError covers an unwritable destination, e.g. a read-only
// checkout.  The error must surface rather than be swallowed into a pass.
func TestSyncWriteError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores file permissions")
	}
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission semantics")
	}

	root, rel := writeTemp(t, "doc.org", "# BEGIN demo:demo\nold\n# END demo:demo\n")
	require.NoError(t, os.Chmod(filepath.Join(root, rel), 0o400))

	s := fixedFragments("demo", []Doc{{Path: rel, Format: FormatOrg}})

	_, err := s.Sync(root, true)
	assert.ErrorIs(t, err, os.ErrPermission)
}

func TestRepoRootNotFound(t *testing.T) {
	_, err := RepoRoot(t.TempDir())
	assert.ErrorContains(t, err, "no go.mod")
}

func TestRepoRootFindsModule(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n"), 0o644))

	nested := filepath.Join(root, "a", "b", "c")
	require.NoError(t, os.MkdirAll(nested, 0o755))

	got, err := RepoRoot(nested)
	require.NoError(t, err)
	assert.Equal(t, root, got)
}

// TestFindFloatUsageBadDir covers a missing directory.
func TestFindFloatUsageBadDir(t *testing.T) {
	_, err := FindFloatUsage(filepath.Join(t.TempDir(), "missing"))
	assert.ErrorIs(t, err, os.ErrNotExist)
}

// TestFindFloatUsageUnparseable covers source the parser rejects.  A file that
// will not parse must be an error, never an implicit pass — otherwise a broken
// file would silently satisfy the float ban.
func TestFindFloatUsageUnparseable(t *testing.T) {
	root, _ := writeTemp(t, "broken.go", "package x\nfunc (\n")

	_, err := FindFloatUsage(root)
	assert.Error(t, err)
}

// TestFindFloatUsageDetects is the positive control: the ban must actually
// catch both an identifier and a literal, and must ignore non-Go files,
// subdirectories, and float mentions inside comments and strings.
func TestFindFloatUsageDetects(t *testing.T) {
	root := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(root, "bad.go"), []byte(
		"package x\n\nfunc f() float64 { return 1.5 }\n"), 0o644))

	// Prose and string literals mentioning float64 must not register.
	require.NoError(t, os.WriteFile(filepath.Join(root, "clean.go"), []byte(
		"package x\n\n// This mentions float64 in a comment.\nconst s = \"float64\"\n"), 0o644))

	// Non-Go files and subdirectories are out of scope.
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes.md"), []byte("float64"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "sub", "deep.go"), []byte(
		"package y\n\nvar v float64\n"), 0o644))

	found, err := FindFloatUsage(root)
	require.NoError(t, err)
	require.Len(t, found, 2, "want the identifier and the literal from bad.go only")

	for _, u := range found {
		assert.Equal(t, "bad.go", u.File)
	}
	assert.Equal(t, "float64", found[0].What)
	assert.Equal(t, "literal 1.5", found[1].What)
}
