package account

import (
	"io"
	"strconv"

	rpt "github.com/rustyeddy/trader/internal/service/accountreport"
	"github.com/rustyeddy/trader/internal/tradejournal"
)

// orgFormatter writes the trading journal. It renders completed trades only;
// the activity commands refuse it up front (requireActivityFormat), and these
// methods refuse it again so no path can print something that looks like a
// journal but is not.
type orgFormatter struct{}

func (orgFormatter) FormatSync(io.Writer, SyncOutput) error { return errOrgTradesOnly }
func (orgFormatter) FormatList(io.Writer, ListOutput) error { return errOrgTradesOnly }

func (orgFormatter) FormatTrades(w io.Writer, out TradesOutput) error {
	if out.Open {
		return errOrgTradesOnly
	}
	r := out.Response
	doc := tradejournal.Document{Preamble: []string{
		"trader account journal: completed trades closed in " + rangeText(r.From, r.To),
		"stream: " + streamText(r.Stream),
	}}
	if r.Benchmark != rpt.BenchmarkNone {
		doc.Preamble = append(doc.Preamble, "benchmark: "+string(r.Benchmark), "account currency: "+currencyText(r.AccountCurrency))
	}
	if n := len(r.Orphans); n > 0 {
		doc.Preamble = append(doc.Preamble, plural(n, "exit", "exits")+" with no known entry, not attributed to any trade")
	}
	if n := len(r.Unattributed); n > 0 {
		doc.Preamble = append(doc.Preamble, plural(n, "financing or fee activity", "financing or fee activities")+" not attributed to any trade")
	}
	if len(r.Trades) == 0 {
		doc.Preamble = append(doc.Preamble, "no completed trades in range")
	}
	for _, row := range r.Trades {
		doc.Entries = append(doc.Entries, tradejournal.Entry{Trade: row.Trade, Quality: journalQuality(row.Quality)})
	}
	return tradejournal.RenderOrg(w, doc)
}

func (orgFormatter) FormatTrade(w io.Writer, d rpt.Detail) error {
	if d.Completed == nil {
		return errOrgTradesOnly
	}
	doc := tradejournal.Document{
		Preamble: []string{"trader account journal: " + d.Completed.ID, "stream: " + streamText(d.Stream)},
		Entries:  []tradejournal.Entry{{Trade: *d.Completed, Quality: journalQuality(d.Quality)}},
	}
	if d.Quality != nil {
		doc.Preamble = append(doc.Preamble, "account currency: "+currencyText(d.AccountCurrency))
	}
	return tradejournal.RenderOrg(w, doc)
}

func journalQuality(q *rpt.Quality) *tradejournal.Quality {
	if q == nil {
		return nil
	}
	out := &tradejournal.Quality{Benchmark: string(q.Benchmark), SlippageCost: q.SlippageCost, Reason: q.Reason}
	for _, f := range q.Fills {
		out.Fills = append(out.Fills, tradejournal.Fill{Source: f.Source, Result: f.Result, Reason: f.Reason})
	}
	return out
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}
