package numericstudy

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// reportSections is the order the report prints its tables in, with the
// heading each one appears under.  The names match the fragment names embedded
// in the ADR and README, so the printed report and the documents are the same
// content rendered once.
var reportSections = []struct{ name, heading string }{
	{"asset-matrix", "Asset representation matrix"},
	{"range-headroom", "Range and headroom per candidate scale"},
	{"notional-all", "Price x Quantity headroom (whole unscaled quantities)"},
	{"rolling-sum", "Rolling-sum capacity"},
	{"subquantum", "Sub-quantum values (valid decimals, not representable at 1e8)"},
	{"unsupported", "Unsupported quotation formats"},
}

// TestGenerateReport prints the validation tables for ADR-004 in org-mode
// syntax, computed from the same data the assertions exercise.
//
// The tables checked into the ADR and README are the same fragments, verified
// by TestGeneratedTables — so this report is for reading, not for pasting.
// To refresh the documents, run that test with -update instead.
//
// Print it to the terminal:
//
//	go test ./test/internal/numericstudy/ -run TestGenerateReport -v
//
// Or write it straight to a file, with no test framing around it:
//
//	NUMERICSTUDY_REPORT=report.org go test ./test/internal/numericstudy/ -run TestGenerateReport
func TestGenerateReport(t *testing.T) {
	frag := Fragments(FormatOrg)

	var b strings.Builder
	for _, s := range reportSections {
		body, ok := frag[s.name]
		require.True(t, ok, "report references unknown fragment %q", s.name)
		fmt.Fprintf(&b, "\n** %s\n\n%s", s.heading, body)
	}
	b.WriteString("\n=—= means the value needs more decimals than the scale holds.\n")

	if path := os.Getenv("NUMERICSTUDY_REPORT"); path != "" {
		require.NoError(t, os.WriteFile(path, []byte(b.String()), 0o644))
		t.Logf("report written to %s", path)
		return
	}

	// Only print when explicitly asked, so a plain `go test ./...` (and
	// therefore `make check`) stays quiet.
	if testing.Verbose() {
		fmt.Print(b.String())
	}
}
