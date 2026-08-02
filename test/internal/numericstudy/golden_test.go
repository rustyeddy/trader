package numericstudy

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// update rewrites the generated regions in the documents below instead of
// asserting on them:
//
//	go test ./test/internal/numericstudy/ -run TestGeneratedTables -update
var update = flag.Bool("update", false,
	"rewrite generated table regions in the ADR and README")

// generatedDoc is a document carrying machine-maintained table regions.
type generatedDoc struct {
	path   string // relative to the repository root
	format Format
}

var generatedDocs = []generatedDoc{
	{path: "docs/arch/adr-004-exact-numeric-representation.org", format: FormatOrg},
	{path: "test/internal/numericstudy/README.md", format: FormatMarkdown},
}

// Marker syntax, in each format's comment form so the markers stay invisible
// when the document is rendered:
//
//	org:      # BEGIN numericstudy:asset-matrix ... # END numericstudy:asset-matrix
//	markdown: <!-- BEGIN numericstudy:asset-matrix --> ... <!-- END ... -->
func markers(f Format, name string) (begin, end string) {
	if f == FormatMarkdown {
		return "<!-- BEGIN numericstudy:" + name + " -->",
			"<!-- END numericstudy:" + name + " -->"
	}
	return "# BEGIN numericstudy:" + name, "# END numericstudy:" + name
}

var regionRe = map[Format]*regexp.Regexp{
	FormatOrg: regexp.MustCompile(
		`(?m)^# BEGIN numericstudy:([a-z0-9-]+)$\n([\s\S]*?)^# END numericstudy:([a-z0-9-]+)$`),
	FormatMarkdown: regexp.MustCompile(
		`(?m)^<!-- BEGIN numericstudy:([a-z0-9-]+) -->$\n([\s\S]*?)^<!-- END numericstudy:([a-z0-9-]+) -->$`),
}

// TestGeneratedTables is the guarantee behind the claim that the checked-in
// tables track the evidence: every marked region in the ADR and README must
// equal the fragment this package generates for it.  Change the asset matrix
// without regenerating and this test fails.
//
// Run with -update to rewrite the regions in place.
func TestGeneratedTables(t *testing.T) {
	root := repoRoot(t)

	for _, doc := range generatedDocs {
		t.Run(doc.path, func(t *testing.T) {
			path := filepath.Join(root, doc.path)
			raw, err := os.ReadFile(path)
			require.NoError(t, err)

			frag := Fragments(doc.format)
			re := regionRe[doc.format]

			seen := map[string]bool{}
			var mismatched []string

			updated := re.ReplaceAllStringFunc(string(raw), func(m string) string {
				sub := re.FindStringSubmatch(m)
				name, body, closing := sub[1], sub[2], sub[3]

				require.Equal(t, name, closing,
					"mismatched BEGIN/END marker names in %s", doc.path)

				want, ok := frag[name]
				if !ok {
					t.Errorf("%s: unknown fragment %q; known: %v",
						doc.path, name, fragmentNames(frag))
					return m
				}
				seen[name] = true

				if body != want {
					mismatched = append(mismatched, name)
				}

				begin, end := markers(doc.format, name)
				return begin + "\n" + want + end
			})

			require.NotEmpty(t, seen,
				"%s has no numericstudy markers; generated tables are unguarded",
				doc.path)

			if *update {
				require.NoError(t, os.WriteFile(path, []byte(updated), 0o644))
				t.Logf("updated %d region(s) in %s", len(seen), doc.path)
				return
			}

			assert.Empty(t, mismatched,
				"%s is stale in %v — regenerate with:\n"+
					"  go test ./test/internal/numericstudy/ -run TestGeneratedTables -update",
				doc.path, mismatched)
		})
	}
}

// TestEveryFragmentIsUsed keeps the generator honest in the other direction: a
// fragment nobody embeds is dead code that will silently rot.
func TestEveryFragmentIsUsed(t *testing.T) {
	root := repoRoot(t)
	used := map[string]bool{}

	for _, doc := range generatedDocs {
		raw, err := os.ReadFile(filepath.Join(root, doc.path))
		require.NoError(t, err)
		for _, m := range regionRe[doc.format].FindAllStringSubmatch(string(raw), -1) {
			used[m[1]] = true
		}
	}

	for name := range Fragments(FormatOrg) {
		assert.True(t, used[name],
			"fragment %q is generated but embedded in no document", name)
	}
}

// TestFragmentsRenderInBothFormats guards the renderer itself: every fragment
// must produce a non-empty, well-formed table in org and markdown alike.
func TestFragmentsRenderInBothFormats(t *testing.T) {
	for _, f := range []Format{FormatOrg, FormatMarkdown} {
		for name, body := range Fragments(f) {
			t.Run(fmt.Sprintf("%d/%s", f, name), func(t *testing.T) {
				lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
				require.GreaterOrEqual(t, len(lines), 3,
					"want header, rule, and at least one row")

				// The org rule row separates columns with "+", so count both
				// delimiters to compare column counts across formats.
				delims := func(s string) int {
					return strings.Count(s, "|") + strings.Count(s, "+")
				}

				want := delims(lines[0])
				for i, ln := range lines {
					assert.True(t, strings.HasPrefix(ln, "|"),
						"line %d must start a table row: %q", i, ln)
					assert.Equal(t, want, delims(ln),
						"line %d has a different column count: %q", i, ln)
				}
			})
		}
	}
}

func fragmentNames(frag map[string]string) []string {
	out := make([]string, 0, len(frag))
	for k := range frag {
		out = append(out, k)
	}
	return out
}

// repoRoot walks up from the package directory to the module root.
func repoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	require.NoError(t, err)

	for range 10 {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, dir, parent, "reached filesystem root without go.mod")
		dir = parent
	}
	t.Fatal("could not locate repository root")
	return ""
}
