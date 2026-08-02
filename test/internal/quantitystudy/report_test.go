package quantitystudy

import (
	"fmt"
	"os"
	"strings"
	"testing"

	ns "github.com/rustyeddy/trader/test/internal/numericstudy"
	"github.com/stretchr/testify/require"
)

// reportSections is the order the report prints its tables in.  The names match
// the fragments embedded in the ADR and README, so the printed report and the
// documents are one source rendered once.
var reportSections = []struct{ name, heading string }{
	{"quantity-matrix", "Quantity representation matrix"},
	{"frontier", "Precision and range frontier"},
	{"instrument-rules", "Instrument rules (separate from representation scale)"},
	{"notional", "Price(1e8) x Quantity headroom, both operands scaled"},
}

// TestGenerateReport prints the validation tables in org-mode syntax.
//
// The checked-in tables are the same fragments, verified by
// TestGeneratedTables — so this is for reading, not for pasting.  To refresh
// the documents, run that test with -update.
//
//	go test ./test/internal/quantitystudy/ -run TestGenerateReport -v
//	NUMERICSTUDY_REPORT=q.org go test ./test/internal/quantitystudy/ -run TestGenerateReport
func TestGenerateReport(t *testing.T) {
	frag := Fragments(ns.FormatOrg)

	var b strings.Builder
	for _, s := range reportSections {
		body, ok := frag[s.name]
		require.True(t, ok, "report references unknown fragment %q", s.name)
		fmt.Fprintf(&b, "\n** %s\n\n%s", s.heading, body)
	}

	fmt.Fprintf(&b, "\nPRECISION = needs more decimals than the scale holds.\n"+
		"RANGE     = exceeds int64 at that scale.\n\n"+
		"Selected scale: %s.  Smallest quantity %s, maximum whole units %s.\n",
		SelectedScale.Name, ns.FormatDecimal(1, SelectedScale),
		ns.Commas(MaxSupportedWholeUnits))

	if path := os.Getenv("NUMERICSTUDY_REPORT"); path != "" {
		require.NoError(t, os.WriteFile(path, []byte(b.String()), 0o644))
		t.Logf("report written to %s", path)
		return
	}

	if testing.Verbose() {
		fmt.Print(b.String())
	}
}
