package quantitystudy

import (
	"errors"
	"fmt"

	ns "github.com/rustyeddy/trader/test/internal/numericstudy"
)

// Frontier reports, for one candidate scale, whether it can hold the finest
// and the largest quantity in the pressure matrix.
//
// No scaled int64 satisfies both.  One satoshi requires 8 fraction digits, and
// a trillion whole units at 8 fraction digits requires 1e20 — 67 bits against
// int64's 63.  This type exists so that fact is computed rather than asserted
// in prose.
//
// The trillion-unit row is a deliberate range-pressure case, not a
// requirement: per #36 it is retained in the evidence to mark the boundary of
// the supported domain, and the recommendation is made on precision coverage
// plus an explicitly bounded range.  See SelectedScale.
type Frontier struct {
	Scale         ns.Scale
	MaxWholeUnits int64
	SmallestUnit  string
	HoldsFinest   bool
	HoldsLargest  bool
}

// Frontiers evaluates every candidate.
func Frontiers() []Frontier {
	out := make([]Frontier, 0, len(Candidates))
	for _, sc := range Candidates {
		out = append(out, Frontier{
			Scale:         sc,
			MaxWholeUnits: MaxWholeUnits(sc),
			SmallestUnit:  ns.FormatDecimal(1, sc),
			HoldsFinest:   Representable(FinestQuantity, sc),
			HoldsLargest:  Representable(LargestQuantity, sc),
		})
	}
	return out
}

// SatisfiesBoth reports whether any candidate holds both extremes.
func SatisfiesBoth() bool {
	for _, f := range Frontiers() {
		if f.HoldsFinest && f.HoldsLargest {
			return true
		}
	}
	return false
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// Fragments renders every generated table for this study, keyed by the name
// used in the document markers.
func Fragments(f ns.Format) map[string]string {
	frag := map[string]string{}

	// Quantity representation matrix -----------------------------------------
	var class, symbol, qty, dec, integral []string
	for _, q := range Quantities {
		class = append(class, q.Class)
		symbol = append(symbol, q.Symbol)
		qty = append(qty, q.Quantity)
		dec = append(dec, fmt.Sprint(q.Decimals))
		integral = append(integral, yesNo(q.Integral))
	}

	cols := []ns.Column{
		{Header: "Class", Cells: class},
		{Header: "Symbol", Cells: symbol},
		{Header: "Quantity", Right: true, Cells: qty},
		{Header: "Dec", Right: true, Cells: dec},
		{Header: "Integral", Cells: integral},
	}
	for _, sc := range Candidates {
		cells := make([]string, 0, len(Quantities))
		for _, q := range Quantities {
			// Precision failures and range failures lead to different
			// conclusions — one narrows the asset classes a scale can serve,
			// the other bounds the supported domain — so they are reported
			// distinctly rather than both as a blank cell.
			v, err := ns.ParseDecimal(q.Quantity, sc)
			switch {
			case errors.Is(err, ns.ErrTooManyDecimals):
				cells = append(cells, "PRECISION")
			case errors.Is(err, ns.ErrOverflow):
				cells = append(cells, "RANGE")
			case err != nil:
				cells = append(cells, "—")
			case ns.FormatDecimal(v, sc) != ns.Canonical(q.Quantity):
				cells = append(cells, "INEXACT")
			default:
				cells = append(cells, fmt.Sprintf("%d", v))
			}
		}
		cols = append(cols, ns.Column{Header: sc.Name, Right: true, Cells: cells})
	}
	frag["quantity-matrix"] = ns.RenderTable(f, cols)

	// Range and precision frontier -------------------------------------------
	var fs, fu, fm, ff, fl []string
	for _, fr := range Frontiers() {
		fs = append(fs, fr.Scale.Name)
		fu = append(fu, fr.SmallestUnit)
		fm = append(fm, ns.Commas(fr.MaxWholeUnits))
		ff = append(ff, yesNo(fr.HoldsFinest))
		fl = append(fl, yesNo(fr.HoldsLargest))
	}
	frag["frontier"] = ns.RenderTable(f, []ns.Column{
		{Header: "Scale", Cells: fs},
		{Header: "Smallest unit", Right: true, Cells: fu},
		{Header: "Max whole units", Right: true, Cells: fm},
		{Header: "Holds 1 satoshi", Cells: ff},
		{Header: "Holds 1e12 units", Cells: fl},
	})

	// Instrument rules -------------------------------------------------------
	var is, ii, im, ix, ig []string
	for _, inc := range Increments {
		is = append(is, inc.Symbol)
		ii = append(ii, inc.Increment)
		im = append(im, inc.Minimum)
		if inc.Maximum == "" {
			ix = append(ix, "—")
		} else {
			ix = append(ix, inc.Maximum)
		}
		ig = append(ig, yesNo(inc.IntegralOnly))
	}
	frag["instrument-rules"] = ns.RenderTable(f, []ns.Column{
		{Header: "Symbol", Cells: is},
		{Header: "Increment", Right: true, Cells: ii},
		{Header: "Minimum", Right: true, Cells: im},
		{Header: "Maximum", Right: true, Cells: ix},
		{Header: "Integral only", Cells: ig},
	})

	// Price x Quantity intermediates -----------------------------------------
	ncols := []ns.Column{
		{Header: "Case", Cells: pluckNotional(func(n NotionalCase) string { return n.Name })},
		{Header: "Price", Right: true, Cells: pluckNotional(func(n NotionalCase) string { return n.Price })},
		{Header: "Quantity", Right: true, Cells: pluckNotional(func(n NotionalCase) string { return n.Quantity })},
	}
	for _, sc := range Candidates {
		cells := make([]string, 0, len(NotionalCases))
		for _, n := range NotionalCases {
			p, perr := ns.ParseDecimal(n.Price, PriceScale)
			q, qerr := ns.ParseDecimal(n.Quantity, sc)
			switch {
			case perr != nil || qerr != nil:
				cells = append(cells, "n/a")
			case NotionalOverflows(p, q):
				cells = append(cells, "OVERFLOW")
			default:
				cells = append(cells, ns.Commas(ns.MaxPriceCeil(p*q))+"x")
			}
		}
		ncols = append(ncols, ns.Column{Header: sc.Name, Right: true, Cells: cells})
	}
	frag["notional"] = ns.RenderTable(f, ncols)

	return frag
}

func pluckNotional(fn func(NotionalCase) string) []string {
	out := make([]string, 0, len(NotionalCases))
	for _, n := range NotionalCases {
		out = append(out, fn(n))
	}
	return out
}
