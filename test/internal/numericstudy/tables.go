package numericstudy

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Format selects the markup a table fragment is rendered in.  The same data
// backs both, so the ADR (org) and README (markdown) cannot disagree.
type Format int

const (
	FormatOrg Format = iota
	FormatMarkdown
)

// Column is one rendered column: a header, an alignment, and its cells.
type Column struct {
	Header string
	Right  bool
	Cells  []string
}

// RenderTable lays out columns as an aligned org or markdown table.  Alignment
// is cosmetic in both formats, but keeps the checked-in docs readable and
// makes regeneration diffs small.
func RenderTable(f Format, cols []Column) string {
	rows := 0
	for _, c := range cols {
		rows = max(rows, len(c.Cells))
	}

	// Column widths are counted in runes, not bytes: the "—" used for
	// unrepresentable values is three bytes wide but one column wide.
	width := make([]int, len(cols))
	for i, c := range cols {
		width[i] = utf8.RuneCountInString(c.Header)
		for _, v := range c.Cells {
			width[i] = max(width[i], utf8.RuneCountInString(v))
		}
	}

	pad := func(i int, s string) string {
		gap := strings.Repeat(" ", width[i]-utf8.RuneCountInString(s))
		if cols[i].Right {
			return gap + s
		}
		return s + gap
	}

	var b strings.Builder

	b.WriteString("|")
	for i, c := range cols {
		b.WriteString(" " + pad(i, c.Header) + " |")
	}
	b.WriteString("\n")

	// Separator: org uses +, markdown uses | and encodes alignment in the rule.
	b.WriteString("|")
	for i := range cols {
		rule := strings.Repeat("-", width[i]+2)
		if f == FormatMarkdown {
			if cols[i].Right {
				rule = strings.Repeat("-", width[i]+1) + ":"
			}
			b.WriteString(rule + "|")
			continue
		}
		b.WriteString(rule)
		if i == len(cols)-1 {
			b.WriteString("|")
		} else {
			b.WriteString("+")
		}
	}
	b.WriteString("\n")

	for r := range rows {
		b.WriteString("|")
		for i, c := range cols {
			v := ""
			if r < len(c.Cells) {
				v = c.Cells[r]
			}
			b.WriteString(" " + pad(i, v) + " |")
		}
		b.WriteString("\n")
	}

	return b.String()
}

// ValidateTable checks that rendered text really is a table: every line is a
// row, and every row has the same column count.  Sibling studies apply it to
// their own fragments, so it returns an error rather than taking a *testing.T.
func ValidateTable(body string) error {
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	if len(lines) < 3 {
		return fmt.Errorf("want header, rule, and at least one row, got %d lines",
			len(lines))
	}

	// The org rule row separates columns with "+", so count both delimiters
	// to compare column counts across formats.
	delims := func(s string) int {
		return strings.Count(s, "|") + strings.Count(s, "+")
	}

	want := delims(lines[0])
	for i, ln := range lines {
		if !strings.HasPrefix(ln, "|") {
			return fmt.Errorf("line %d is not a table row: %q", i, ln)
		}
		if got := delims(ln); got != want {
			return fmt.Errorf("line %d has %d columns, want %d: %q",
				i, got, want, ln)
		}
	}

	return nil
}

// code wraps an inline literal in the target format's verbatim markup.
func code(f Format, s string) string {
	if f == FormatMarkdown {
		return "`" + s + "`"
	}
	return "=" + s + "="
}

// Commas groups an integer with thousands separators.
func Commas(v int64) string {
	s := fmt.Sprintf("%d", v)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}

	var parts []string
	for len(s) > 3 {
		parts = append([]string{s[len(s)-3:]}, parts...)
		s = s[:len(s)-3]
	}
	parts = append([]string{s}, parts...)

	out := strings.Join(parts, ",")
	if neg {
		out = "-" + out
	}
	return out
}

// scaledCells renders one asset's stored value at every candidate scale.
func scaledCells(price string) []string {
	out := make([]string, 0, len(Candidates))
	for _, sc := range Candidates {
		v, err := ParseDecimal(price, sc)
		switch {
		case err != nil:
			out = append(out, "—")
		case FormatDecimal(v, sc) != Canonical(price):
			out = append(out, "INEXACT")
		default:
			out = append(out, fmt.Sprintf("%d", v))
		}
	}
	return out
}

// Fragments renders every generated table, keyed by the name used in the
// document markers.  Adding a fragment here makes it available to any document
// that opens a matching marker pair.
func Fragments(f Format) map[string]string {
	frag := map[string]string{}

	// Asset representation matrix -------------------------------------------
	cols := []Column{
		{Header: "Class", Cells: pluck(Assets, func(a Asset) string { return a.Class })},
		{Header: "Symbol", Cells: pluck(Assets, func(a Asset) string { return a.Symbol })},
		{Header: "Price", Right: true, Cells: pluck(Assets, func(a Asset) string { return a.Price })},
		{Header: "Tick", Right: true, Cells: pluck(Assets, func(a Asset) string { return a.TickSize })},
		{Header: "Dec", Right: true, Cells: pluck(Assets, func(a Asset) string { return fmt.Sprint(a.Decimals) })},
	}
	for i, sc := range Candidates {
		cells := make([]string, 0, len(Assets))
		for _, a := range Assets {
			cells = append(cells, scaledCells(a.Price)[i])
		}
		cols = append(cols, Column{Header: sc.Name, Right: true, Cells: cells})
	}
	frag["asset-matrix"] = RenderTable(f, cols)

	// Range and headroom ----------------------------------------------------
	var name, dec, maxp, unit, head []string
	for _, sc := range Candidates {
		name = append(name, sc.Name)
		dec = append(dec, fmt.Sprint(sc.Decimals))
		maxp = append(maxp, Commas(MaxPrice(sc)))
		unit = append(unit, FormatDecimal(1, sc))
		if v, err := ParseDecimal("750000.00", sc); err == nil {
			head = append(head, Commas(Headroom(v, sc))+"x")
		} else {
			head = append(head, "n/a")
		}
	}
	frag["range-headroom"] = RenderTable(f, []Column{
		{Header: "Scale", Cells: name},
		{Header: "Decimals", Right: true, Cells: dec},
		{Header: "Max price", Right: true, Cells: maxp},
		{Header: "Smallest unit", Right: true, Cells: unit},
		{Header: "Headroom over 750000.00", Right: true, Cells: head},
	})

	// Price x Quantity margins ----------------------------------------------
	frag["notional-all"] = notionalTable(f, Candidates)
	frag["notional-8v9"] = notionalTable(f, []Scale{
		mustScale("1e8"), mustScale("1e9"),
	})

	// Rolling-sum capacity --------------------------------------------------
	var rsScale, rsBars []string
	for _, sc := range Candidates {
		v, err := ParseDecimal("750000.00", sc)
		if err != nil {
			continue
		}
		rsScale = append(rsScale, sc.Name)
		rsBars = append(rsBars, Commas(MaxPriceCeil(v)))
	}
	frag["rolling-sum"] = RenderTable(f, []Column{
		{Header: "Scale", Cells: rsScale},
		{Header: "Bars of 750000.00 before overflow", Right: true, Cells: rsBars},
	})

	// Sub-quantum values ----------------------------------------------------
	frag["subquantum"] = RenderTable(f, []Column{
		{Header: "Value", Cells: pluckSub(func(i int) string { return code(f, SubQuantumValues[i].Value) })},
		{Header: "Digits", Right: true, Cells: pluckSub(func(i int) string { return fmt.Sprint(SubQuantumValues[i].Digits) })},
		{Header: "Note", Cells: pluckSub(func(i int) string { return SubQuantumValues[i].Note })},
	})

	// Unsupported quotation formats -----------------------------------------
	var uf, ue, ud, ur []string
	for _, q := range UnsupportedQuotations {
		uf = append(uf, q.Format)
		ue = append(ue, code(f, q.Example))
		ud = append(ud, q.Decimal)
		ur = append(ur, q.Reason)
	}
	frag["unsupported"] = RenderTable(f, []Column{
		{Header: "Format", Cells: uf},
		{Header: "Example", Cells: ue},
		{Header: "Normalizes to", Right: true, Cells: ud},
		{Header: "Reason", Cells: ur},
	})

	return frag
}

func notionalTable(f Format, scales []Scale) string {
	cols := []Column{
		{Header: "Case", Cells: pluckNotional(func(n Notional) string { return n.Name })},
		{Header: "Quantity", Right: true, Cells: pluckNotional(func(n Notional) string { return Commas(n.Quantity) })},
	}
	for _, sc := range scales {
		cells := make([]string, 0, len(Notionals))
		for _, n := range Notionals {
			p, err := ParseDecimal(n.Price, sc)
			switch {
			case err != nil:
				cells = append(cells, "n/a")
			case MulOverflows(p, n.Quantity):
				cells = append(cells, "OVERFLOW")
			default:
				cells = append(cells, Commas(MaxPriceCeil(p*n.Quantity))+"x")
			}
		}
		cols = append(cols, Column{Header: sc.Name, Right: true, Cells: cells})
	}
	return RenderTable(f, cols)
}

// MaxPriceCeil reports how many times v divides into the int64 ceiling — the
// margin remaining above an already-computed intermediate.
func MaxPriceCeil(v int64) int64 {
	if v <= 0 {
		return 0
	}
	return maxInt64 / v
}

const maxInt64 = int64(^uint64(0) >> 1)

func mustScale(name string) Scale {
	for _, sc := range Candidates {
		if sc.Name == name {
			return sc
		}
	}
	panic("numericstudy: unknown scale " + name)
}

func pluck(in []Asset, fn func(Asset) string) []string {
	out := make([]string, 0, len(in))
	for _, a := range in {
		out = append(out, fn(a))
	}
	return out
}

func pluckNotional(fn func(Notional) string) []string {
	out := make([]string, 0, len(Notionals))
	for _, n := range Notionals {
		out = append(out, fn(n))
	}
	return out
}

func pluckSub(fn func(int) string) []string {
	out := make([]string, 0, len(SubQuantumValues))
	for i := range SubQuantumValues {
		out = append(out, fn(i))
	}
	return out
}
