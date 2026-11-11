package tradejournal

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/rustyeddy/trader/internal/accountactivity"
	"github.com/rustyeddy/trader/internal/executionquality"
	"github.com/rustyeddy/trader/internal/tradehistory"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

const unknownText = "unknown"

// Document is what RenderOrg writes: some context lines, then one entry per
// completed trade in the order given.
type Document struct {
	// Preamble lines are written as Org comments before the first entry.
	Preamble []string
	Entries  []Entry
}

// Entry is one completed trade and, only if the caller measured it, its
// execution quality. A nil Quality means "not requested", which is not the
// same as unknown.
type Entry struct {
	Trade   tradehistory.Trade
	Quality *Quality
}

// Quality is a trade's execution quality against one benchmark.
type Quality struct {
	// Benchmark names the benchmark the caller asked for.
	Benchmark string
	Fills     []Fill
	// SlippageCost is the trade total, with Reason saying why it is unknown.
	SlippageCost accountactivity.Value[num.Money]
	Reason       string
}

// Fill is one fill measured against the benchmark. Result is nil when the
// measurement could not be made; Reason then says why.
type Fill struct {
	Source accountactivity.Key
	Result *executionquality.Result
	Reason string
}

// reviewFields is the scaffold the trader fills in. Trader never writes into it.
var reviewFields = []string{"Setup", "What went well", "What went wrong", "Screenshot", "Notes"}

// RenderOrg writes the document as Org-mode text. The whole document is built
// first and written once, so a failed write is the only error.
func RenderOrg(w io.Writer, d Document) error {
	var b strings.Builder
	for _, l := range d.Preamble {
		b.WriteString("# " + flat(l) + "\n")
	}
	if len(d.Preamble) > 0 && len(d.Entries) > 0 {
		b.WriteString("\n")
	}
	for i, e := range d.Entries {
		if i > 0 {
			b.WriteString("\n")
		}
		writeEntry(&b, e)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func writeEntry(b *strings.Builder, e Entry) {
	t := e.Trade
	prop := func(name, value string) { b.WriteString(":" + name + ": " + value + "\n") }

	b.WriteString("* " + flat(timeText(t.OpenedAt)+" "+t.Instrument.String()+" "+sideText(t.Side)) + "\n")
	b.WriteString(":PROPERTIES:\n")
	prop("TRADE_ID", flat(t.ID))
	prop("INSTRUMENT", flat(t.Instrument.String()))
	prop("SIDE", sideText(t.Side))
	prop("OPENED", timeText(t.OpenedAt))
	prop("CLOSED", timeText(t.ClosedAt))
	prop("QUANTITY", t.EntryQuantity.String())
	prop("AVG_ENTRY", prov(t.AvgEntryPrice, priceText(t.AvgEntryPrice)))
	prop("AVG_EXIT", prov(t.AvgExitPrice, priceText(t.AvgExitPrice)))
	prop("GROSS_PL", prov(t.GrossPL, moneyText(t.GrossPL)))
	prop("SPREAD_COST", prov(t.SpreadCost, moneyText(t.SpreadCost)))
	prop("COMMISSION", prov(t.Commission, moneyText(t.Commission)))
	prop("FINANCING", prov(t.Financing, moneyText(t.Financing)))
	prop("OTHER_FEES", prov(t.OtherFees, moneyText(t.OtherFees)))
	prop("NET", prov(t.Net, moneyText(t.Net)))
	if e.Quality != nil {
		prop("BENCHMARK", flat(e.Quality.Benchmark))
		prop("SLIPPAGE_COST", prov(e.Quality.SlippageCost, moneyText(e.Quality.SlippageCost)))
	}
	if n := len(t.Sources); n > 0 {
		prop("SOURCES_FIRST", flat(t.Sources[0].String()))
		prop("SOURCES_LAST", flat(t.Sources[n-1].String()))
	}
	prop("SOURCES_COUNT", fmt.Sprint(len(t.Sources)))
	b.WriteString(":END:\n\n")

	b.WriteString("** Execution\n")
	b.WriteString("Spread cost is already inside GROSS_PL and is not added to NET. " +
		"NET is GROSS_PL + COMMISSION + FINANCING + OTHER_FEES, known only when all four are.\n\n")
	writeLegs(b, t)
	if e.Quality != nil {
		b.WriteString("\n")
		writeQuality(b, e.Quality)
	}

	b.WriteString("\n** Review\n")
	for _, f := range reviewFields {
		b.WriteString("- " + f + ":\n")
	}
}

func writeLegs(b *strings.Builder, t tradehistory.Trade) {
	b.WriteString("| Leg | Time | Quantity | Price | Broker trade | Division | Source |\n|-\n")
	row := func(name string, ls []tradehistory.Leg) {
		for _, l := range ls {
			b.WriteString(cells(name, timeText(l.At), l.Quantity.String(), prov(l.Price, priceText(l.Price)),
				legTrade(l), legDivision(l), l.Source.String()))
		}
	}
	row("entry", t.Entries)
	row("exit", t.Exits)
}

func writeQuality(b *strings.Builder, q *Quality) {
	b.WriteString("*** Execution quality\n")
	b.WriteString("Benchmark: " + flat(q.Benchmark) + ". Slippage is account-centric: negative is worse for the trader.\n\n")
	b.WriteString("| Fill | Execution price | Benchmark | Age | Slippage | Slippage cost |\n|-\n")
	var notes []string
	for _, f := range q.Fills {
		if f.Result == nil {
			b.WriteString(cells(f.Source.String(), unknownText, unknownText, unknownText, unknownText, unknownText))
			notes = append(notes, f.Source.String()+": unmeasured: "+f.Reason)
			continue
		}
		r := f.Result
		bench, age := unknownText, unknownText
		if r.Benchmark != nil {
			bench = fmt.Sprintf("%s %s (%s)", r.Benchmark.Kind, decimal(r.Benchmark.Price.String()), r.Benchmark.Source)
		}
		if r.AgeKnown {
			age = r.BenchmarkAge.String()
		}
		b.WriteString(cells(f.Source.String(), prov(r.ExecutionPrice, priceText(r.ExecutionPrice)), bench, age,
			prov(r.Slippage, rateText(r.Slippage)), prov(r.SlippageCost, moneyText(r.SlippageCost))))
		for _, u := range r.Unresolved {
			notes = append(notes, f.Source.String()+": "+u)
		}
	}
	if len(notes) > 0 {
		b.WriteString("\n")
		for _, n := range notes {
			b.WriteString("- " + flat(n) + "\n")
		}
	}
	b.WriteString("\nTrade slippage cost: " + prov(q.SlippageCost, moneyText(q.SlippageCost)) + "\n")
	if !q.SlippageCost.Known() && q.Reason != "" {
		b.WriteString("Why unknown: " + flat(q.Reason) + "\n")
	}
}

// cells renders one Org table row. A bar inside a cell would split it.
func cells(cs ...string) string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = strings.ReplaceAll(flat(c), "|", `\vert{}`)
	}
	return "| " + strings.Join(out, " | ") + " |\n"
}

// flat makes outside text one line: control characters, newlines included,
// become spaces, so it can never begin a heading or close a drawer.
func flat(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == 0x85 || r == ' ' || r == ' ' {
			return ' '
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}

func sideText(s order.Side) string {
	if s == order.Buy {
		return "long"
	}
	return "short"
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

// prov marks a derived figure as derived; an unknown figure is only the word
// unknown, and a reported one carries no mark. How it was derived is the audit
// view's business (trader account trades show), not the journal's.
func prov[T any](v accountactivity.Value[T], text string) string {
	if !v.Known() {
		return unknownText
	}
	if v.Basis() == accountactivity.BasisDerived {
		return text + " (derived)"
	}
	return text
}

// decimal renders a canonical decimal with at least two fraction digits, so a
// known zero reads 0.00 and can never be mistaken for unknown.
func decimal(s string) string {
	s, _, _ = strings.Cut(s, " ")
	whole, frac, _ := strings.Cut(s, ".")
	for len(frac) < 2 {
		frac += "0"
	}
	return whole + "." + frac
}

func moneyText(v accountactivity.Value[num.Money]) string {
	m, ok := v.Get()
	if !ok {
		return unknownText
	}
	return decimal(m.String()) + " " + m.Currency().String()
}

func priceText(v accountactivity.Value[num.Price]) string {
	p, ok := v.Get()
	if !ok {
		return unknownText
	}
	return decimal(p.String())
}

func rateText(v accountactivity.Value[num.Rate]) string {
	r, ok := v.Get()
	if !ok {
		return unknownText
	}
	return decimal(r.String())
}

// timeText renders the stored instant in UTC at full precision.
func timeText(t time.Time) string {
	if t.IsZero() {
		return unknownText
	}
	return t.UTC().Format(time.RFC3339Nano)
}
