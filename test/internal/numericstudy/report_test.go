package numericstudy

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestGenerateReport prints the validation tables for ADR-004 in org-mode
// syntax, generated from the same data the assertions above exercise so the
// document cannot drift from the evidence.
//
// Print it to the terminal:
//
//	go test ./test/internal/numericstudy/ -run TestGenerateReport -v
//
// Or write it straight to a file, with no test framing around it:
//
//	NUMERICSTUDY_REPORT=report.org go test ./test/internal/numericstudy/ -run TestGenerateReport
//
// The report goes to stdout via fmt rather than t.Log so the org tables come
// out unindented and paste directly into the ADR.
func TestGenerateReport(t *testing.T) {
	var b strings.Builder

	b.WriteString("\n** Asset representation matrix\n\n")
	b.WriteString("| Class | Symbol | Price | Tick | Dec |")
	for _, sc := range Candidates {
		b.WriteString(" " + sc.Name + " stored |")
	}
	b.WriteString("\n|---\n")

	for _, a := range Assets {
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %d |",
			a.Class, a.Symbol, a.Price, a.TickSize, a.Decimals)
		for _, sc := range Candidates {
			v, err := ParseDecimal(a.Price, sc)
			if err != nil {
				b.WriteString(" — |")
				continue
			}
			if got := FormatDecimal(v, sc); got != Canonical(a.Price) {
				fmt.Fprintf(&b, " INEXACT(%s) |", got)
				continue
			}
			fmt.Fprintf(&b, " %d |", v)
		}
		b.WriteString("\n")
	}

	b.WriteString("\n=—= means the value needs more decimals than the scale holds.\n")

	b.WriteString("\n** Range and headroom per candidate scale\n\n")
	b.WriteString("| Scale | Decimals | Max price | Smallest unit | Headroom over 750000.00 |\n|---\n")
	for _, sc := range Candidates {
		smallest := FormatDecimal(1, sc)
		head := "n/a"
		if v, err := ParseDecimal("750000.00", sc); err == nil {
			head = fmt.Sprintf("%dx", Headroom(v, sc))
		}
		fmt.Fprintf(&b, "| %s | %d | %s | %s | %s |\n",
			sc.Name, sc.Decimals, MaxPriceText(sc), smallest, head)
	}

	b.WriteString("\n** Intermediate arithmetic headroom\n\n")
	b.WriteString("| Case | Quantity |")
	for _, sc := range Candidates {
		b.WriteString(" " + sc.Name + " |")
	}
	b.WriteString("\n|---\n")
	for _, n := range Notionals {
		fmt.Fprintf(&b, "| %s | %d |", n.Name, n.Quantity)
		for _, sc := range Candidates {
			p, err := ParseDecimal(n.Price, sc)
			if err != nil {
				b.WriteString(" n/a |")
				continue
			}
			if MulOverflows(p, n.Quantity) {
				b.WriteString(" OVERFLOW |")
				continue
			}
			margin := float64(maxInt64) / float64(p*n.Quantity)
			fmt.Fprintf(&b, " %.0fx spare |", margin)
		}
		b.WriteString("\n")
	}

	b.WriteString("\n** Rolling-sum capacity (bars of 750000.00 before int64 overflow)\n\n")
	b.WriteString("| Scale | Bars |\n|---\n")
	for _, sc := range Candidates {
		v, err := ParseDecimal("750000.00", sc)
		if err != nil {
			continue
		}
		fmt.Fprintf(&b, "| %s | %d |\n", sc.Name, maxInt64/v)
	}

	b.WriteString("\n** Unsupported quotation formats\n\n")
	b.WriteString("| Format | Example | Normalized decimal | Reason |\n|---\n")
	for _, q := range UnsupportedQuotations {
		fmt.Fprintf(&b, "| %s | =%s= | %s | %s |\n",
			q.Format, q.Example, q.Decimal, q.Reason)
	}

	if path := os.Getenv("NUMERICSTUDY_REPORT"); path != "" {
		require.NoError(t, os.WriteFile(path, []byte(b.String()), 0o644))
		t.Logf("report written to %s", path)
		return
	}
	fmt.Print(b.String())
}

const maxInt64 = int64(^uint64(0) >> 1)
