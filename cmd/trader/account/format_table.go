package account

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/rustyeddy/trader/internal/accountactivity"
	rpt "github.com/rustyeddy/trader/internal/service/accountreport"
	"github.com/rustyeddy/trader/internal/tradehistory"
	"github.com/rustyeddy/trader/num"
)

// tableFormatter is the default, human-readable Formatter. Unknown figures
// print as the word "unknown" and known zeros as 0.00, everywhere.
type tableFormatter struct{}

func tab(w io.Writer) *tabwriter.Writer { return tabwriter.NewWriter(w, 0, 0, 2, ' ', 0) }

func streamText(k accountactivity.SyncKey) string {
	return k.Provider + "/" + k.Environment.String() + "/" + k.ProviderAccountID
}

// prov renders a value with where it came from; an unknown value is only the
// word unknown.
func prov[T any](v accountactivity.Value[T], text string) string {
	if !v.Known() {
		return unknownText
	}
	switch v.Basis() {
	case accountactivity.BasisDerived:
		return text + " [derived: " + v.Source() + "]"
	default:
		if v.Source() != "" {
			return text + " [reported: " + v.Source() + "]"
		}
		return text + " [reported]"
	}
}

func rangeText(from, to time.Time) string {
	switch {
	case from.IsZero() && to.IsZero():
		return "all"
	case to.IsZero():
		return "from " + timeText(from)
	case from.IsZero():
		return "before " + timeText(to)
	default:
		return "[" + timeText(from) + ", " + timeText(to) + ")"
	}
}

func (tableFormatter) FormatSync(w io.Writer, out SyncOutput) error {
	ew := &errWriter{w: w}
	tw := tab(ew)
	r := out.Result
	pf(tw, "stream:\t%s\n", streamText(out.Stream))
	pf(tw, "pages:\t%d\n", r.Pages)
	pf(tw, "fetched:\t%d\n", r.Fetched)
	pf(tw, "new:\t%d\n", r.Added)
	pf(tw, "unchanged:\t%d\n", r.Unchanged)
	pf(tw, "other events:\t%d\n", r.Other)
	pf(tw, "cursor:\t%s\n", orNone(r.Cursor))
	if out.HasCheckpoint {
		pf(tw, "checkpoint:\t%s\n", timeText(out.Checkpoint.UpdatedAt))
	} else {
		pf(tw, "checkpoint:\tnone\n")
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	return ew.err
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

// amounts renders the monetary fields that apply to an activity's kind. A
// field that applies but was not supplied is unknown; one that does not
// apply to the kind is not printed.
func amounts(a accountactivity.Activity) string {
	type f struct {
		name string
		v    accountactivity.Value[num.Money]
	}
	var fields []f
	switch a.Kind {
	case accountactivity.KindFill:
		fields = []f{{"realized_pl", a.RealizedPL}, {"spread_cost", a.SpreadCost}, {"commission", a.Commission}, {"financing", a.Financing}, {"other_fees", a.OtherFees}}
	case accountactivity.KindFinancing:
		fields = []f{{"financing", a.Financing}}
	case accountactivity.KindFee:
		fields = []f{{"other_fees", a.OtherFees}}
	case accountactivity.KindOther:
		return ""
	default:
		fields = []f{{"cash", a.Cash}}
	}
	parts := make([]string, len(fields))
	for i, x := range fields {
		parts[i] = x.name + "=" + moneyText(x.v)
	}
	return strings.Join(parts, " ")
}

func (tableFormatter) FormatList(w io.Writer, out ListOutput) error {
	ew := &errWriter{w: w}
	r := out.Result
	pf(ew, "stream: %s\nrange:  %s\n", streamText(r.Stream), rangeText(out.From, out.To))
	if r.HasCheckpoint {
		pf(ew, "synced: cursor %s at %s\n", r.Checkpoint.Cursor, timeText(r.Checkpoint.UpdatedAt))
	} else {
		pf(ew, "synced: never\n")
	}
	if len(r.Activities) == 0 {
		pf(ew, "\nno activity in range\n")
		return ew.err
	}
	pl(ew)
	tw := tab(ew)
	pl(tw, "TIME\tKIND\tID\tTYPE\tINSTRUMENT\tSIDE\tQUANTITY\tPRICE\tAMOUNTS")
	for _, a := range r.Activities {
		side, qty, price := "", "", ""
		if a.Kind == accountactivity.KindFill {
			side = strings.ToLower(a.Side.String())
			qty = qtyValueText(a.Quantity)
			price = priceText(a.Price)
		}
		pf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			timeText(a.OccurredAt), a.Kind, a.ProviderActivityID, a.ProviderType,
			a.Instrument.String(), side, qty, price, amounts(a))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if r.Truncated() {
		pf(ew, "\nshowing the first %d of %d matching activities (--limit %d)\n", len(r.Activities), r.Matched, r.Limit)
	}
	return ew.err
}

func currencyText(c rpt.Currency) string {
	if !c.Known {
		return unknownText
	}
	return c.Code.String() + " (" + c.Source + ")"
}

func (tableFormatter) FormatTrades(w io.Writer, out TradesOutput) error {
	ew := &errWriter{w: w}
	r := out.Response
	pf(ew, "stream:           %s\n", streamText(r.Stream))
	if out.Open {
		pf(ew, "view:             open positions\n")
	} else {
		if r.From.IsZero() && r.To.IsZero() {
			pf(ew, "view:             completed trades\n")
		} else {
			pf(ew, "view:             completed trades closed in %s\n", rangeText(r.From, r.To))
		}
	}
	if r.Benchmark != rpt.BenchmarkNone {
		pf(ew, "benchmark:        %s\naccount currency: %s\n", r.Benchmark, currencyText(r.AccountCurrency))
	}
	pl(ew)

	var notes []string
	if out.Open {
		writeOpenTable(ew, r.Open)
	} else {
		notes = writeTradesTable(ew, r)
	}
	if len(notes) > 0 {
		pf(ew, "\nunknown slippage cost:\n")
		for _, n := range notes {
			pf(ew, "  %s\n", n)
		}
	}
	if len(r.Orphans) > 0 {
		pf(ew, "\nexits with no known entry (not attributed to any trade):\n")
		for _, o := range r.Orphans {
			pf(ew, "  %s  %s units\n", o.Source, o.Quantity)
		}
	}
	if len(r.Unattributed) > 0 {
		pf(ew, "\nfinancing and fees not attributed to any trade:\n")
		for _, k := range r.Unattributed {
			pf(ew, "  %s\n", k)
		}
	}
	return ew.err
}

func writeOpenTable(w io.Writer, open []tradehistory.OpenPosition) {
	if len(open) == 0 {
		pl(w, "no open positions")
		return
	}
	tw := tab(w)
	pl(tw, "ID\tINSTRUMENT\tSIDE\tOPENED\tREMAINING\tAVG_ENTRY")
	for _, o := range open {
		pf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", o.ID, o.Instrument.String(), strings.ToLower(o.Side.String()),
			timeText(o.OpenedAt), o.Remaining, priceText(o.AvgEntryPrice))
	}
	_ = tw.Flush()
}

func writeTradesTable(w io.Writer, r rpt.TradesResponse) []string {
	if len(r.Trades) == 0 {
		pl(w, "no completed trades in range")
		return nil
	}
	quality := r.Benchmark != rpt.BenchmarkNone
	tw := tab(w)
	head := "ID\tINSTRUMENT\tSIDE\tOPENED\tCLOSED\tQUANTITY\tENTRY\tEXIT\tGROSS_PL\tSPREAD_COST\tCOMMISSION\tFINANCING\tOTHER_FEES\tNET"
	if quality {
		head += "\tSLIPPAGE_COST"
	}
	pl(tw, head)
	var notes []string
	for _, row := range r.Trades {
		t := row.Trade
		line := fmt.Sprintf("%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s",
			t.ID, t.Instrument.String(), strings.ToLower(t.Side.String()), timeText(t.OpenedAt), timeText(t.ClosedAt),
			t.EntryQuantity, priceText(t.AvgEntryPrice), priceText(t.AvgExitPrice),
			moneyText(t.GrossPL), moneyText(t.SpreadCost), moneyText(t.Commission), moneyText(t.Financing), moneyText(t.OtherFees), moneyText(t.Net))
		if quality {
			line += "\t" + moneyText(row.Quality.SlippageCost)
			if !row.Quality.SlippageCost.Known() {
				notes = append(notes, t.ID+": "+row.Quality.Reason)
			}
		}
		pl(tw, line)
	}
	_ = tw.Flush()
	return notes
}

func (tableFormatter) FormatTrade(w io.Writer, d rpt.Detail) error {
	ew := &errWriter{w: w}
	kv := tab(ew)
	row := func(k, v string) { pf(kv, "%s:\t%s\n", k, v) }

	row("stream", streamText(d.Stream))
	if t := d.Completed; t != nil {
		row("trade", t.ID)
		row("status", "completed")
		row("instrument", t.Instrument.String())
		row("side", strings.ToLower(t.Side.String()))
		row("opened", timeText(t.OpenedAt))
		row("closed", timeText(t.ClosedAt))
		row("quantity", t.EntryQuantity.String())
		row("avg entry price", prov(t.AvgEntryPrice, priceText(t.AvgEntryPrice)))
		row("avg exit price", prov(t.AvgExitPrice, priceText(t.AvgExitPrice)))
		row("gross p/l", prov(t.GrossPL, moneyText(t.GrossPL)))
		row("spread cost", prov(t.SpreadCost, moneyText(t.SpreadCost))+"  (already inside gross p/l; not added to net)")
		row("commission", prov(t.Commission, moneyText(t.Commission)))
		row("financing", prov(t.Financing, moneyText(t.Financing)))
		row("other fees", prov(t.OtherFees, moneyText(t.OtherFees)))
		row("net", prov(t.Net, moneyText(t.Net)))
	}
	if o := d.Open; o != nil {
		row("trade", o.ID)
		row("status", "open")
		row("instrument", o.Instrument.String())
		row("side", strings.ToLower(o.Side.String()))
		row("opened", timeText(o.OpenedAt))
		row("remaining", o.Remaining.String())
		row("avg entry price", prov(o.AvgEntryPrice, priceText(o.AvgEntryPrice)))
	}
	if err := kv.Flush(); err != nil {
		return err
	}

	var entries, exits []tradehistory.Leg
	if d.Completed != nil {
		entries, exits = d.Completed.Entries, d.Completed.Exits
	} else {
		entries, exits = d.Open.Entries, d.Open.Exits
	}
	pf(ew, "\nlegs:\n")
	lt := tab(ew)
	pl(lt, "  LEG\tTIME\tQUANTITY\tPRICE\tBROKER_TRADE\tDIVISION\tSOURCE")
	legs := func(name string, ls []tradehistory.Leg) {
		for _, l := range ls {
			pf(lt, "  %s\t%s\t%s\t%s\t%s\t%s\t%s\n", name, timeText(l.At), l.Quantity, prov(l.Price, priceText(l.Price)),
				legTrade(l), legDivision(l), l.Source)
		}
	}
	legs("entry", entries)
	legs("exit", exits)
	if err := lt.Flush(); err != nil {
		return err
	}

	pf(ew, "\nsource activities:\n")
	for _, a := range d.Fills {
		writeActivityDetail(ew, a)
	}
	if d.Quality != nil {
		writeQualityDetail(ew, d)
	}
	return ew.err
}

func legTrade(l tradehistory.Leg) string {
	switch {
	case l.BrokerTradeID != "":
		return l.BrokerTradeID
	case len(l.CandidateTradeIDs) > 0:
		return "one of " + strings.Join(l.CandidateTradeIDs, ",")
	default:
		return "none"
	}
}

func legDivision(l tradehistory.Leg) string {
	if l.Split {
		return "derived from net position"
	}
	return "broker"
}

func writeActivityDetail(w io.Writer, a accountactivity.Activity) {
	pf(w, "  %s\n", a.Key())
	tw := tab(w)
	line := func(k, v string) { pf(tw, "    %s:\t%s\n", k, v) }
	line("time", timeText(a.OccurredAt))
	line("type", a.ProviderType)
	line("side", strings.ToLower(a.Side.String()))
	line("quantity", qtyValueText(a.Quantity))
	line("fill price", prov(a.Price, priceText(a.Price)))
	if a.FullVWAP.Known() {
		line("full vwap", prov(a.FullVWAP, priceText(a.FullVWAP)))
	}
	line("realized p/l", prov(a.RealizedPL, moneyText(a.RealizedPL)))
	line("spread cost", prov(a.SpreadCost, moneyText(a.SpreadCost)))
	line("commission", prov(a.Commission, moneyText(a.Commission)))
	line("financing", prov(a.Financing, moneyText(a.Financing)))
	line("other fees", prov(a.OtherFees, moneyText(a.OtherFees)))
	if a.BrokerOrderID != "" {
		line("broker order", a.BrokerOrderID)
	}
	for _, e := range a.TradeEffects {
		line("trade effect", fmt.Sprintf("%s %s units=%s price=%s", e.BrokerTradeID, e.Role, qtyValueText(e.Units), priceText(e.Price)))
	}
	_ = tw.Flush()
}

func writeQualityDetail(w io.Writer, d rpt.Detail) {
	q := d.Quality
	pf(w, "\nexecution quality (benchmark %s, account currency %s):\n", q.Benchmark, currencyText(d.AccountCurrency))
	for _, f := range q.Fills {
		pf(w, "  %s\n", f.Source)
		if f.Result == nil {
			pf(w, "    unmeasured: %s\n", f.Reason)
			continue
		}
		r := f.Result
		tw := tab(w)
		line := func(k, v string) { pf(tw, "    %s:\t%s\n", k, v) }
		line("execution price", prov(r.ExecutionPrice, priceText(r.ExecutionPrice)))
		if r.Benchmark != nil {
			line("benchmark", fmt.Sprintf("%s %s (%s)", r.Benchmark.Kind, decimal(r.Benchmark.Price.String()), r.Benchmark.Source))
		}
		if r.AgeKnown {
			line("benchmark age", r.BenchmarkAge.String())
		}
		line("slippage", prov(r.Slippage, rateText(r.Slippage)))
		line("slippage cost", prov(r.SlippageCost, moneyText(r.SlippageCost)))
		if len(r.Unresolved) > 0 {
			line("unresolved", strings.Join(r.Unresolved, "; "))
		}
		_ = tw.Flush()
	}
	pf(w, "  trade slippage cost: %s\n", prov(q.SlippageCost, moneyText(q.SlippageCost)))
	if !q.SlippageCost.Known() {
		pf(w, "    why unknown: %s\n", q.Reason)
	}
}
