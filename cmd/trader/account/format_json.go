package account

import (
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/rustyeddy/trader/internal/accountactivity"
	"github.com/rustyeddy/trader/internal/executionquality"
	rpt "github.com/rustyeddy/trader/internal/service/accountreport"
	"github.com/rustyeddy/trader/internal/tradehistory"
	"github.com/rustyeddy/trader/num"
)

// schemaVersion versions the JSON this package emits. It is an API: scripts,
// MCP and reports consume it, so a change to a field's meaning or name bumps
// it. Adding a field does not.
const schemaVersion = 1

// jsonFormatter writes the stable JSON form. Numbers are decimal strings,
// never floats; times are RFC 3339 UTC; slices are [] and never null; and a
// figure is either {"state":"known",...} or {"state":"unknown"}.
type jsonFormatter struct{}

func (jsonFormatter) encode(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// jval is one figure with its provenance.
type jval struct {
	State    string `json:"state"`
	Value    string `json:"value,omitempty"`
	Currency string `json:"currency,omitempty"`
	Basis    string `json:"basis,omitempty"`
	Source   string `json:"source,omitempty"`
}

func known[T any](v accountactivity.Value[T], value, currency string) jval {
	return jval{State: "known", Value: value, Currency: currency, Basis: v.Basis().String(), Source: v.Source()}
}

func jMoney(v accountactivity.Value[num.Money]) jval {
	m, ok := v.Get()
	if !ok {
		return jval{State: "unknown"}
	}
	return known(v, decimal(m.String()), m.Currency().String())
}

func jPrice(v accountactivity.Value[num.Price]) jval {
	p, ok := v.Get()
	if !ok {
		return jval{State: "unknown"}
	}
	return known(v, decimal(p.String()), "")
}

func jRate(v accountactivity.Value[num.Rate]) jval {
	r, ok := v.Get()
	if !ok {
		return jval{State: "unknown"}
	}
	return known(v, decimal(r.String()), "")
}

func jQty(v accountactivity.Value[num.Quantity]) jval {
	q, ok := v.Get()
	if !ok {
		return jval{State: "unknown"}
	}
	return known(v, q.String(), "")
}

type jStream struct {
	Provider    string `json:"provider"`
	Environment string `json:"environment"`
	Account     string `json:"account"`
}

func jStreamOf(k accountactivity.SyncKey) jStream {
	return jStream{Provider: k.Provider, Environment: k.Environment.String(), Account: k.ProviderAccountID}
}

func tstr(t time.Time) string { return timeText(t) }

type jRange struct {
	From string `json:"from,omitempty"`
	To   string `json:"to,omitempty"`
}

// ---- sync ----

type jSync struct {
	SchemaVersion int     `json:"schema_version"`
	Stream        jStream `json:"stream"`
	Pages         int     `json:"pages"`
	Fetched       int     `json:"fetched"`
	New           int     `json:"new"`
	Unchanged     int     `json:"unchanged"`
	OtherEvents   int     `json:"other_events"`
	Cursor        string  `json:"cursor"`
	CheckpointAt  string  `json:"checkpoint_at,omitempty"`
}

func (f jsonFormatter) FormatSync(w io.Writer, o SyncOutput) error {
	out := jSync{
		SchemaVersion: schemaVersion, Stream: jStreamOf(o.Stream),
		Pages: o.Result.Pages, Fetched: o.Result.Fetched, New: o.Result.Added, Unchanged: o.Result.Unchanged,
		OtherEvents: o.Result.Other, Cursor: o.Result.Cursor,
	}
	if o.HasCheckpoint {
		out.CheckpointAt = tstr(o.Checkpoint.UpdatedAt)
	}
	return f.encode(w, out)
}

// ---- list ----

type jActivity struct {
	Key           string `json:"key"`
	ActivityID    string `json:"activity_id"`
	ProviderType  string `json:"provider_type"`
	Kind          string `json:"kind"`
	OccurredAt    string `json:"occurred_at"`
	Instrument    string `json:"instrument,omitempty"`
	Side          string `json:"side,omitempty"`
	Quantity      *jval  `json:"quantity,omitempty"`
	Price         *jval  `json:"price,omitempty"`
	FullVWAP      *jval  `json:"full_vwap,omitempty"`
	RealizedPL    *jval  `json:"realized_pl,omitempty"`
	SpreadCost    *jval  `json:"spread_cost,omitempty"`
	Commission    *jval  `json:"commission,omitempty"`
	Financing     *jval  `json:"financing,omitempty"`
	OtherFees     *jval  `json:"other_fees,omitempty"`
	Cash          *jval  `json:"cash,omitempty"`
	BrokerOrderID string `json:"broker_order_id,omitempty"`
	BrokerBatchID string `json:"broker_batch_id,omitempty"`
	TradeEffects  []jEff `json:"trade_effects"`
	// ExecutionQuote is the broker's whole quote snapshot, present only when
	// the fill recorded one.
	ExecutionQuote *jQuote `json:"execution_quote,omitempty"`
	// Recorded* are slippage fields stored on the activity itself, present only
	// when it carries them. The values execution quality derives on demand are
	// under execution_quality, not here.
	RecordedBenchmark    *jval `json:"recorded_benchmark,omitempty"`
	RecordedSlippage     *jval `json:"recorded_slippage,omitempty"`
	RecordedSlippageCost *jval `json:"recorded_slippage_cost,omitempty"`
}

type jLevel struct {
	Price     string `json:"price"`
	Liquidity string `json:"liquidity"`
}

type jQuote struct {
	Basis       string   `json:"basis"`
	Source      string   `json:"source,omitempty"`
	At          string   `json:"at,omitempty"`
	Bids        []jLevel `json:"bids"`
	Asks        []jLevel `json:"asks"`
	CloseoutBid jval     `json:"closeout_bid"`
	CloseoutAsk jval     `json:"closeout_ask"`
}

func jLevels(ls []accountactivity.PriceLevel) []jLevel {
	out := make([]jLevel, 0, len(ls))
	for _, l := range ls {
		out = append(out, jLevel{Price: decimal(l.Price.String()), Liquidity: l.Liquidity.String()})
	}
	return out
}

// jEff is one broker trade a fill touched, with every recorded fact and its
// provenance: reversal cost allocation and execution-quality interpretation
// rest on these.
type jEff struct {
	BrokerTradeID          string `json:"broker_trade_id"`
	Role                   string `json:"role"`
	Units                  jval   `json:"units"`
	Price                  jval   `json:"price"`
	RealizedPL             jval   `json:"realized_pl"`
	Financing              jval   `json:"financing"`
	SpreadCost             jval   `json:"spread_cost"`
	GuaranteedExecutionFee jval   `json:"guaranteed_execution_fee"`
}

func ptr(v jval) *jval { return &v }

// jActivityOf includes exactly the figures that apply to the activity's kind,
// each as known or unknown, so a figure that applies but is missing is
// visible as unknown and one that does not apply is simply absent.
func jActivityOf(a accountactivity.Activity) jActivity {
	j := jActivity{
		Key: a.Key().String(), ActivityID: a.ProviderActivityID, ProviderType: a.ProviderType,
		Kind: a.Kind.String(), OccurredAt: tstr(a.OccurredAt), Instrument: a.Instrument.String(),
		BrokerOrderID: a.BrokerOrderID, BrokerBatchID: a.BrokerBatchID, TradeEffects: []jEff{},
	}
	switch a.Kind {
	case accountactivity.KindFill:
		j.Side = strings.ToLower(a.Side.String())
		j.Quantity, j.Price = ptr(jQty(a.Quantity)), ptr(jPrice(a.Price))
		if a.FullVWAP.Known() {
			j.FullVWAP = ptr(jPrice(a.FullVWAP))
		}
		j.RealizedPL, j.SpreadCost = ptr(jMoney(a.RealizedPL)), ptr(jMoney(a.SpreadCost))
		j.Commission, j.Financing, j.OtherFees = ptr(jMoney(a.Commission)), ptr(jMoney(a.Financing)), ptr(jMoney(a.OtherFees))
	case accountactivity.KindFinancing:
		j.Financing = ptr(jMoney(a.Financing))
	case accountactivity.KindFee:
		j.Commission, j.OtherFees = ptr(jMoney(a.Commission)), ptr(jMoney(a.OtherFees))
	case accountactivity.KindOther:
	default:
		j.Cash = ptr(jMoney(a.Cash))
	}
	for _, e := range a.TradeEffects {
		j.TradeEffects = append(j.TradeEffects, jEff{
			BrokerTradeID: e.BrokerTradeID, Role: e.Role.String(), Units: jQty(e.Units), Price: jPrice(e.Price),
			RealizedPL: jMoney(e.RealizedPL), Financing: jMoney(e.Financing), SpreadCost: jMoney(e.SpreadCost),
			GuaranteedExecutionFee: jMoney(e.GuaranteedExecutionFee),
		})
	}
	if q, ok := a.ExecutionQuote.Get(); ok {
		j.ExecutionQuote = &jQuote{
			Basis: a.ExecutionQuote.Basis().String(), Source: a.ExecutionQuote.Source(), At: tstr(q.At),
			Bids: jLevels(q.Bids), Asks: jLevels(q.Asks), CloseoutBid: jPrice(q.CloseoutBid), CloseoutAsk: jPrice(q.CloseoutAsk),
		}
	}
	if a.Benchmark.Known() {
		j.RecordedBenchmark = ptr(jPrice(a.Benchmark))
	}
	if a.Slippage.Known() {
		j.RecordedSlippage = ptr(jRate(a.Slippage))
	}
	if a.SlippageCost.Known() {
		j.RecordedSlippageCost = ptr(jMoney(a.SlippageCost))
	}
	return j
}

type jList struct {
	SchemaVersion int         `json:"schema_version"`
	Stream        jStream     `json:"stream"`
	Range         jRange      `json:"range"`
	Checkpoint    *jCheck     `json:"checkpoint,omitempty"`
	Matched       int         `json:"matched"`
	Limit         int         `json:"limit"`
	Truncated     bool        `json:"truncated"`
	Activities    []jActivity `json:"activities"`
}

type jCheck struct {
	Cursor    string `json:"cursor"`
	UpdatedAt string `json:"updated_at"`
}

func (f jsonFormatter) FormatList(w io.Writer, o ListOutput) error {
	r := o.Result
	out := jList{
		SchemaVersion: schemaVersion, Stream: jStreamOf(r.Stream), Range: jRange{From: tstr(o.From), To: tstr(o.To)},
		Matched: r.Matched, Limit: r.Limit, Truncated: r.Truncated(), Activities: []jActivity{},
	}
	if r.HasCheckpoint {
		out.Checkpoint = &jCheck{Cursor: r.Checkpoint.Cursor, UpdatedAt: tstr(r.Checkpoint.UpdatedAt)}
	}
	for _, a := range r.Activities {
		out.Activities = append(out.Activities, jActivityOf(a))
	}
	return f.encode(w, out)
}

// ---- trades ----

type jLeg struct {
	Source            string   `json:"source"`
	At                string   `json:"at"`
	Quantity          string   `json:"quantity"`
	Price             jval     `json:"price"`
	BrokerTradeID     string   `json:"broker_trade_id,omitempty"`
	CandidateTradeIDs []string `json:"candidate_trade_ids,omitempty"`
	// DivisionDerived is true when the closing/opening division of the fill
	// came from the net position, not from broker facts.
	DivisionDerived bool `json:"division_derived"`
}

func jLegs(ls []tradehistory.Leg) []jLeg {
	out := make([]jLeg, 0, len(ls))
	for _, l := range ls {
		out = append(out, jLeg{
			Source: l.Source.String(), At: tstr(l.At), Quantity: l.Quantity.String(), Price: jPrice(l.Price),
			BrokerTradeID: l.BrokerTradeID, CandidateTradeIDs: l.CandidateTradeIDs, DivisionDerived: l.Split,
		})
	}
	return out
}

func keyStrings(ks []accountactivity.Key) []string {
	out := make([]string, 0, len(ks))
	for _, k := range ks {
		out = append(out, k.String())
	}
	return out
}

type jTrade struct {
	ID               string    `json:"id"`
	Instrument       string    `json:"instrument"`
	Side             string    `json:"side"`
	OpenedAt         string    `json:"opened_at"`
	ClosedAt         string    `json:"closed_at"`
	Quantity         string    `json:"quantity"`
	AvgEntryPrice    jval      `json:"avg_entry_price"`
	AvgExitPrice     jval      `json:"avg_exit_price"`
	GrossPL          jval      `json:"gross_pl"`
	SpreadCost       jval      `json:"spread_cost"`
	Commission       jval      `json:"commission"`
	Financing        jval      `json:"financing"`
	OtherFees        jval      `json:"other_fees"`
	Net              jval      `json:"net"`
	Entries          []jLeg    `json:"entries"`
	Exits            []jLeg    `json:"exits"`
	Sources          []string  `json:"sources"`
	ExecutionQuality *jQuality `json:"execution_quality,omitempty"`
}

func jTradeOf(t tradehistory.Trade) jTrade {
	return jTrade{
		ID: t.ID, Instrument: t.Instrument.String(), Side: strings.ToLower(t.Side.String()),
		OpenedAt: tstr(t.OpenedAt), ClosedAt: tstr(t.ClosedAt), Quantity: t.EntryQuantity.String(),
		AvgEntryPrice: jPrice(t.AvgEntryPrice), AvgExitPrice: jPrice(t.AvgExitPrice),
		GrossPL: jMoney(t.GrossPL), SpreadCost: jMoney(t.SpreadCost), Commission: jMoney(t.Commission),
		Financing: jMoney(t.Financing), OtherFees: jMoney(t.OtherFees), Net: jMoney(t.Net),
		Entries: jLegs(t.Entries), Exits: jLegs(t.Exits), Sources: keyStrings(t.Sources),
	}
}

type jOpen struct {
	ID            string   `json:"id"`
	Instrument    string   `json:"instrument"`
	Side          string   `json:"side"`
	OpenedAt      string   `json:"opened_at"`
	Remaining     string   `json:"remaining"`
	AvgEntryPrice jval     `json:"avg_entry_price"`
	Entries       []jLeg   `json:"entries"`
	Exits         []jLeg   `json:"exits"`
	Sources       []string `json:"sources"`
}

func jOpenOf(o tradehistory.OpenPosition) jOpen {
	return jOpen{
		ID: o.ID, Instrument: o.Instrument.String(), Side: strings.ToLower(o.Side.String()),
		OpenedAt: tstr(o.OpenedAt), Remaining: o.Remaining.String(), AvgEntryPrice: jPrice(o.AvgEntryPrice),
		Entries: jLegs(o.Entries), Exits: jLegs(o.Exits), Sources: keyStrings(o.Sources),
	}
}

type jCurrency struct {
	State  string `json:"state"`
	Code   string `json:"code,omitempty"`
	Source string `json:"source,omitempty"`
}

func jCurrencyOf(c rpt.Currency) jCurrency {
	if !c.Known {
		return jCurrency{State: "unknown"}
	}
	return jCurrency{State: "known", Code: c.Code.String(), Source: c.Source}
}

type jFillQuality struct {
	Source         string   `json:"source"`
	State          string   `json:"state"` // measured or unmeasured
	Reason         string   `json:"reason,omitempty"`
	ExecutionPrice *jval    `json:"execution_price,omitempty"`
	Benchmark      *jBench  `json:"benchmark,omitempty"`
	BenchmarkAgeMS *int64   `json:"benchmark_age_ms,omitempty"`
	Slippage       *jval    `json:"slippage,omitempty"`
	SlippageCost   *jval    `json:"slippage_cost,omitempty"`
	Unresolved     []string `json:"unresolved"`
}

type jBench struct {
	Kind       string `json:"kind"`
	Price      string `json:"price"`
	At         string `json:"at,omitempty"`
	Source     string `json:"source"`
	IntervalMS int64  `json:"interval_ms,omitempty"`
}

type jQuality struct {
	Benchmark    string         `json:"benchmark"`
	SlippageCost jval           `json:"slippage_cost"`
	Reason       string         `json:"reason,omitempty"`
	Fills        []jFillQuality `json:"fills"`
}

func jBenchOf(b *executionquality.Benchmark) *jBench {
	if b == nil {
		return nil
	}
	return &jBench{Kind: string(b.Kind), Price: decimal(b.Price.String()), At: tstr(b.At), Source: b.Source, IntervalMS: b.Interval.Milliseconds()}
}

func jQualityOf(q *rpt.Quality) *jQuality {
	if q == nil {
		return nil
	}
	out := &jQuality{Benchmark: string(q.Benchmark), SlippageCost: jMoney(q.SlippageCost), Reason: q.Reason, Fills: []jFillQuality{}}
	for _, f := range q.Fills {
		jf := jFillQuality{Source: f.Source.String(), State: "unmeasured", Reason: f.Reason, Unresolved: []string{}}
		if r := f.Result; r != nil {
			jf.State = "measured"
			jf.ExecutionPrice = ptr(jPrice(r.ExecutionPrice))
			jf.Benchmark = jBenchOf(r.Benchmark)
			if r.AgeKnown {
				ms := r.BenchmarkAge.Milliseconds()
				jf.BenchmarkAgeMS = &ms
			}
			jf.Slippage, jf.SlippageCost = ptr(jRate(r.Slippage)), ptr(jMoney(r.SlippageCost))
			if r.Unresolved != nil {
				jf.Unresolved = r.Unresolved
			}
		}
		out.Fills = append(out.Fills, jf)
	}
	return out
}

type jOrphan struct {
	Source   string `json:"source"`
	Quantity string `json:"quantity"`
}

// jTrades is the completed-trades view and jOpenView the open-positions view;
// each carries only its own list, so an empty list always means "none".
type jTrades struct {
	SchemaVersion   int       `json:"schema_version"`
	View            string    `json:"view"`
	Stream          jStream   `json:"stream"`
	Range           jRange    `json:"range"`
	Benchmark       string    `json:"benchmark,omitempty"`
	AccountCurrency jCurrency `json:"account_currency"`
	Trades          []jTrade  `json:"trades"`
	Orphans         []jOrphan `json:"orphans"`
	Unattributed    []string  `json:"unattributed"`
}

type jOpenView struct {
	SchemaVersion   int       `json:"schema_version"`
	View            string    `json:"view"`
	Stream          jStream   `json:"stream"`
	AccountCurrency jCurrency `json:"account_currency"`
	Open            []jOpen   `json:"open_positions"`
	Orphans         []jOrphan `json:"orphans"`
	Unattributed    []string  `json:"unattributed"`
}

func jOrphans(os []tradehistory.Orphan) []jOrphan {
	out := make([]jOrphan, 0, len(os))
	for _, o := range os {
		out = append(out, jOrphan{Source: o.Source.String(), Quantity: o.Quantity.String()})
	}
	return out
}

func (f jsonFormatter) FormatTrades(w io.Writer, o TradesOutput) error {
	r := o.Response
	if o.Open {
		out := jOpenView{
			SchemaVersion: schemaVersion, View: "open", Stream: jStreamOf(r.Stream), AccountCurrency: jCurrencyOf(r.AccountCurrency),
			Open: make([]jOpen, 0, len(r.Open)), Orphans: jOrphans(r.Orphans), Unattributed: keyStrings(r.Unattributed),
		}
		for _, p := range r.Open {
			out.Open = append(out.Open, jOpenOf(p))
		}
		return f.encode(w, out)
	}
	out := jTrades{
		SchemaVersion: schemaVersion, View: "completed", Stream: jStreamOf(r.Stream), Range: jRange{From: tstr(r.From), To: tstr(r.To)},
		Benchmark: string(r.Benchmark), AccountCurrency: jCurrencyOf(r.AccountCurrency),
		Trades: make([]jTrade, 0, len(r.Trades)), Orphans: jOrphans(r.Orphans), Unattributed: keyStrings(r.Unattributed),
	}
	for _, row := range r.Trades {
		t := jTradeOf(row.Trade)
		t.ExecutionQuality = jQualityOf(row.Quality)
		out.Trades = append(out.Trades, t)
	}
	return f.encode(w, out)
}

// ---- show ----

type jDetail struct {
	SchemaVersion    int         `json:"schema_version"`
	Stream           jStream     `json:"stream"`
	AccountCurrency  jCurrency   `json:"account_currency"`
	Status           string      `json:"status"`
	Trade            *jTrade     `json:"trade,omitempty"`
	OpenPosition     *jOpen      `json:"open_position,omitempty"`
	Fills            []jActivity `json:"source_activities"`
	ExecutionQuality *jQuality   `json:"execution_quality,omitempty"`
}

func (f jsonFormatter) FormatTrade(w io.Writer, d rpt.Detail) error {
	out := jDetail{
		SchemaVersion: schemaVersion, Stream: jStreamOf(d.Stream), AccountCurrency: jCurrencyOf(d.AccountCurrency),
		Fills: make([]jActivity, 0, len(d.Fills)), ExecutionQuality: jQualityOf(d.Quality),
	}
	if d.Completed != nil {
		out.Status = "completed"
		t := jTradeOf(*d.Completed)
		out.Trade = &t
	}
	if d.Open != nil {
		out.Status = "open"
		o := jOpenOf(*d.Open)
		out.OpenPosition = &o
	}
	for _, a := range d.Fills {
		out.Fills = append(out.Fills, jActivityOf(a))
	}
	return f.encode(w, out)
}
