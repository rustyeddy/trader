package numericstudy

import (
	"flag"
	"fmt"
	"os"
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

// syncer describes where this study's tables are embedded.  The machinery
// itself lives in golden.go so sibling studies can reuse it.
var syncer = Syncer{
	Namespace: "numericstudy",
	Docs: []Doc{
		{Path: "docs/arch/adr-004-exact-numeric-representation.org", Format: FormatOrg},
		{Path: "test/internal/numericstudy/README.md", Format: FormatMarkdown},
	},
	Fragments: Fragments,
}

// TestGeneratedTables is the guarantee behind the claim that the checked-in
// tables track the evidence: every marked region in the ADR and README must
// equal the fragment this package generates for it.  Change the asset matrix
// without regenerating and this test fails.
//
// Run with -update to rewrite the regions in place.
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
					"  go test ./test/internal/numericstudy/ -run TestGeneratedTables -update")
		})
	}
}

// TestEveryFragmentIsUsed keeps the generator honest in the other direction: a
// fragment nobody embeds is dead code that will silently rot.
func TestEveryFragmentIsUsed(t *testing.T) {
	used, err := syncer.UsedFragments(testRepoRoot(t))
	require.NoError(t, err)

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
				assert.NoError(t, ValidateTable(body))
			})
		}
	}
}

func testRepoRoot(t *testing.T) string {
	t.Helper()

	wd, err := os.Getwd()
	require.NoError(t, err)

	root, err := RepoRoot(wd)
	require.NoError(t, err)

	return root
}
