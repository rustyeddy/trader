package quantitystudy

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"testing"

	ns "github.com/rustyeddy/trader/test/internal/numericstudy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// update rewrites the generated regions instead of asserting on them:
//
//	go test ./test/internal/quantitystudy/ -run TestGeneratedTables -update
var update = flag.Bool("update", false,
	"rewrite generated table regions in the ADR and README")

// syncer describes where this study's tables are embedded.  The "quantitystudy"
// namespace keeps its markers distinct from numericstudy's in the same ADR.
var syncer = ns.Syncer{
	Namespace: "quantitystudy",
	Docs: []ns.Doc{
		{Path: "docs/arch/adr-004-exact-numeric-representation.org", Format: ns.FormatOrg},
		{Path: "test/internal/quantitystudy/README.md", Format: ns.FormatMarkdown},
	},
	Fragments: Fragments,
}

// TestGeneratedTables holds the checked-in tables to the evidence: every marked
// region must equal what this package generates.  Run with -update to rewrite.
func TestGeneratedTables(t *testing.T) {
	root := testRepoRoot(t)

	results, err := syncer.Sync(root, *update)
	require.NoError(t, err)
	require.Len(t, results, len(syncer.Docs))

	for _, res := range results {
		t.Run(res.Doc.Path, func(t *testing.T) {
			assert.Empty(t, res.Unbalanced, "mismatched BEGIN/END marker names")
			assert.Empty(t, res.Unknown, "regions naming a fragment nothing generates")
			require.NotEmpty(t, res.Seen,
				"no %s markers; generated tables are unguarded", syncer.Namespace)

			if *update {
				t.Logf("synced %d region(s)", len(res.Seen))
				return
			}

			assert.Empty(t, res.Stale,
				"stale regions — regenerate with:\n"+
					"  go test ./test/internal/quantitystudy/ -run TestGeneratedTables -update")
		})
	}
}

// TestEveryFragmentIsUsed catches a generated table that no document embeds.
func TestEveryFragmentIsUsed(t *testing.T) {
	used, err := syncer.UsedFragments(testRepoRoot(t))
	require.NoError(t, err)

	for name := range Fragments(ns.FormatOrg) {
		assert.True(t, used[name],
			"fragment %q is generated but embedded in no document", name)
	}
}

// TestMarkersDoNotCollide checks that this study's markers never match
// numericstudy's, so the two can share ADR-004 safely.
func TestMarkersDoNotCollide(t *testing.T) {
	other := ns.Syncer{Namespace: "numericstudy"}

	for _, f := range []ns.Format{ns.FormatOrg, ns.FormatMarkdown} {
		mine, _ := syncer.Markers(f, "frontier")
		theirs, _ := other.Markers(f, "frontier")

		assert.NotEqual(t, mine, theirs)
		assert.Contains(t, mine, "quantitystudy:")
		assert.False(t, strings.Contains(mine, "numericstudy:"),
			"quantitystudy markers must not be a substring match for numericstudy")
	}
}

// TestFragmentsRenderInBothFormats guards the renderer for this study's data.
func TestFragmentsRenderInBothFormats(t *testing.T) {
	for _, f := range []ns.Format{ns.FormatOrg, ns.FormatMarkdown} {
		for name, body := range Fragments(f) {
			t.Run(fmt.Sprintf("%d/%s", f, name), func(t *testing.T) {
				assert.NoError(t, ns.ValidateTable(body))
			})
		}
	}
}

func testRepoRoot(t *testing.T) string {
	t.Helper()

	wd, err := os.Getwd()
	require.NoError(t, err)

	root, err := ns.RepoRoot(wd)
	require.NoError(t, err)

	return root
}
