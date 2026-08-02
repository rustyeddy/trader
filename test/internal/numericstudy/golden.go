package numericstudy

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
)

// This file holds the machine-maintained-documentation machinery shared by the
// numeric design studies.  It lives in a normal file rather than a _test.go so
// sibling study packages can import it — Go does not export test files across
// package boundaries.  It deliberately takes no *testing.T: it computes
// results, and each study's test asserts on them.

// Doc is a document carrying generated table regions.
type Doc struct {
	Path   string // relative to the repository root
	Format Format
}

// Syncer renders a study's table fragments into marked regions of documents
// and reports whether the checked-in text still matches.
//
// Namespace keys the markers so several studies can write into the same
// document without colliding: numericstudy owns "numericstudy:<fragment>" and
// a quantity study owns "quantitystudy:<fragment>".
type Syncer struct {
	Namespace string
	Docs      []Doc
	Fragments func(Format) map[string]string
}

// Markers returns the begin and end marker lines for a fragment, in each
// format's comment syntax so they stay invisible when the document renders:
//
//	org:      # BEGIN numericstudy:asset-matrix
//	markdown: <!-- BEGIN numericstudy:asset-matrix -->
func (s Syncer) Markers(f Format, name string) (begin, end string) {
	if f == FormatMarkdown {
		return "<!-- BEGIN " + s.Namespace + ":" + name + " -->",
			"<!-- END " + s.Namespace + ":" + name + " -->"
	}
	return "# BEGIN " + s.Namespace + ":" + name,
		"# END " + s.Namespace + ":" + name
}

func (s Syncer) regionRe(f Format) *regexp.Regexp {
	ns := regexp.QuoteMeta(s.Namespace)
	if f == FormatMarkdown {
		return regexp.MustCompile(
			`(?m)^<!-- BEGIN ` + ns + `:([a-z0-9-]+) -->$\n([\s\S]*?)^<!-- END ` + ns + `:([a-z0-9-]+) -->$`)
	}
	return regexp.MustCompile(
		`(?m)^# BEGIN ` + ns + `:([a-z0-9-]+)$\n([\s\S]*?)^# END ` + ns + `:([a-z0-9-]+)$`)
}

// SyncResult reports what one document contained and what it should contain.
type SyncResult struct {
	Doc        Doc
	Seen       []string // fragment names found, sorted
	Stale      []string // regions whose text differs from the generated fragment
	Unknown    []string // regions naming a fragment the study does not generate
	Unbalanced []string // regions whose BEGIN and END names disagree
	Content    string   // the document with every region regenerated
	Changed    bool     // whether Content differs from what was on disk
}

// SyncDoc regenerates every marked region in one document.  It never writes;
// callers decide what to do with the result.
func (s Syncer) SyncDoc(root string, d Doc) (SyncResult, error) {
	res := SyncResult{Doc: d}

	raw, err := os.ReadFile(filepath.Join(root, d.Path))
	if err != nil {
		return res, err
	}

	frag := s.Fragments(d.Format)
	re := s.regionRe(d.Format)
	seen := map[string]bool{}

	res.Content = re.ReplaceAllStringFunc(string(raw), func(m string) string {
		sub := re.FindStringSubmatch(m)
		name, body, closing := sub[1], sub[2], sub[3]

		if name != closing {
			res.Unbalanced = append(res.Unbalanced, name+"/"+closing)
			return m
		}

		want, ok := frag[name]
		if !ok {
			res.Unknown = append(res.Unknown, name)
			return m
		}

		seen[name] = true
		if body != want {
			res.Stale = append(res.Stale, name)
		}

		begin, end := s.Markers(d.Format, name)
		return begin + "\n" + want + end
	})

	for name := range seen {
		res.Seen = append(res.Seen, name)
	}
	sort.Strings(res.Seen)
	sort.Strings(res.Stale)
	res.Changed = res.Content != string(raw)

	return res, nil
}

// Sync regenerates every document.  With write set, stale documents are
// rewritten in place.
func (s Syncer) Sync(root string, write bool) ([]SyncResult, error) {
	var out []SyncResult

	for _, d := range s.Docs {
		res, err := s.SyncDoc(root, d)
		if err != nil {
			return nil, err
		}
		if write && res.Changed {
			path := filepath.Join(root, d.Path)
			if err := os.WriteFile(path, []byte(res.Content), 0o644); err != nil {
				return nil, err
			}
		}
		out = append(out, res)
	}

	return out, nil
}

// UsedFragments reports which fragment names are embedded across all of the
// study's documents, so a generated-but-unused fragment can be caught.
func (s Syncer) UsedFragments(root string) (map[string]bool, error) {
	used := map[string]bool{}

	for _, d := range s.Docs {
		raw, err := os.ReadFile(filepath.Join(root, d.Path))
		if err != nil {
			return nil, err
		}
		for _, m := range s.regionRe(d.Format).FindAllStringSubmatch(string(raw), -1) {
			used[m[1]] = true
		}
	}

	return used, nil
}

// RepoRoot walks up from dir to the directory holding go.mod.
func RepoRoot(dir string) (string, error) {
	for range 10 {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("numericstudy: no go.mod above %s", dir)
}
