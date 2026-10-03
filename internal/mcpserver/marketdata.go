package mcpserver

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	marketruntime "github.com/rustyeddy/trader/internal/marketdata"
	svcmarketdata "github.com/rustyeddy/trader/internal/service/marketdata"
)

// The read-only market-data tools (#436). Each is a thin adapter: it
// parses the request with the service layer's shared parsers, calls one
// MarketData operation, and translates the per-symbol results. A bad
// interval, range, provider, or empty symbol list fails the whole call;
// a bad symbol fails only its own entry. Neither tool converts,
// downloads, or writes, and neither returns bars.

// InstrumentsInput is trader_instruments' request.
type InstrumentsInput struct {
	Symbols  []string `json:"symbols" jsonschema:"symbols to resolve, for example EURUSD (FX) or SPY (equity/ETF)"`
	Provider string   `json:"provider,omitempty" jsonschema:"market-data provider (oanda, alpaca, or stooq); defaults to the server's provider"`
}

// InstrumentsOutput is trader_instruments' result: one entry per
// requested symbol, in request order.
type InstrumentsOutput struct {
	Provider    string             `json:"provider"`
	Instruments []InstrumentOutput `json:"instruments"`
}

// InstrumentOutput is one resolved symbol, or the reason it failed.
type InstrumentOutput struct {
	Symbol         string `json:"symbol"`
	InstrumentID   string `json:"instrument_id,omitempty"`
	Kind           string `json:"kind,omitempty" jsonschema:"fx, equity, or etf"`
	Exchange       string `json:"exchange,omitempty"`
	ProviderSymbol string `json:"provider_symbol,omitempty" jsonschema:"the provider's own symbol for the listing"`
	Error          string `json:"error,omitempty"`
}

// CoverageInput is trader_marketdata_coverage's request.
type CoverageInput struct {
	Symbols  []string `json:"symbols" jsonschema:"symbols to report on, for example EURUSD or SPY"`
	Provider string   `json:"provider,omitempty" jsonschema:"market-data provider (oanda, alpaca, or stooq); defaults to the server's provider"`
	Interval string   `json:"interval" jsonschema:"bar interval: M1, H1, H4, D1, or W1"`
	From     string   `json:"from,omitempty" jsonschema:"range start, YYYY-MM-DD or RFC3339; give with to, or omit both for each symbol's existing canonical span"`
	To       string   `json:"to,omitempty" jsonschema:"range end (exclusive), YYYY-MM-DD or RFC3339"`
}

// CoverageOutput is trader_marketdata_coverage's result: one entry per
// requested symbol, in request order.
type CoverageOutput struct {
	Provider string           `json:"provider"`
	Interval string           `json:"interval"`
	Results  []SymbolCoverage `json:"results"`
}

// SymbolCoverage is one symbol's coverage, or the reason it failed.
type SymbolCoverage struct {
	Symbol       string `json:"symbol"`
	InstrumentID string `json:"instrument_id,omitempty"`
	// Range is the range reported on. Omitted when no range was given
	// and the symbol has no canonical data yet.
	Range      *TimeSpan         `json:"range,omitempty"`
	Partitions []PartitionOutput `json:"partitions,omitempty"`
	Gaps       []GapOutput       `json:"gaps,omitempty"`
	Raw        *DataSpanOutput   `json:"raw,omitempty" jsonschema:"raw provider data held, if any"`
	Canonical  *DataSpanOutput   `json:"canonical,omitempty" jsonschema:"canonical data built, if any"`
	Error      string            `json:"error,omitempty"`
}

// TimeSpan is a half-open [start, end) range in RFC3339.
type TimeSpan struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

// PartitionOutput is one calendar month's canonical partition.
type PartitionOutput struct {
	Month    string `json:"month" jsonschema:"YYYY-MM"`
	Status   string `json:"status" jsonschema:"missing, invalid, stale, or current"`
	BarCount int    `json:"bar_count,omitempty"`
}

// GapOutput is one run of absent bars within a current partition.
type GapOutput struct {
	State string `json:"state"`
	Start string `json:"start"`
	End   string `json:"end"`
}

// DataSpanOutput summarizes data held: the first and last bar open times,
// the exclusive end of the last bar, and the monthly partition count.
type DataSpanOutput struct {
	First      string `json:"first,omitempty"`
	Last       string `json:"last,omitempty"`
	End        string `json:"end,omitempty"`
	Partitions int    `json:"partitions"`
}

func (s *server) registerMarketData(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "trader_instruments",
		Description: "Resolve symbols to Trader instruments for a market-data provider: instrument ID, kind (fx, equity, etf), " +
			"exchange, and the provider's symbol. FX providers take 6-letter pairs; equity providers take SPY, QQQ, or AAPL. " +
			"Read-only; one result per symbol.",
	}, s.instruments)
	mcp.AddTool(srv, &mcp.Tool{
		Name: "trader_marketdata_coverage",
		Description: "Report canonical market-data coverage for symbols at one interval: monthly partitions and their status, " +
			"gaps, and the raw and canonical data held. Omit from/to to cover each symbol's existing canonical data. " +
			"Read-only (never converts or downloads) and never returns bars; one result per symbol.",
	}, s.coverage)
}

func (s *server) instruments(ctx context.Context, _ *mcp.CallToolRequest, in InstrumentsInput) (*mcp.CallToolResult, InstrumentsOutput, error) {
	md, err := s.marketData(in.Provider)
	if err != nil {
		return nil, InstrumentsOutput{}, err
	}
	resp, err := md.ResolveInstruments(ctx, svcmarketdata.ResolveInstrumentsRequest{Instruments: instrumentRequests(in.Symbols)})
	if err != nil {
		return nil, InstrumentsOutput{}, err
	}
	out := InstrumentsOutput{Provider: md.Provider(), Instruments: make([]InstrumentOutput, len(resp.Results))}
	for i, r := range resp.Results {
		o := InstrumentOutput{Symbol: r.Request.Symbol}
		if r.Err != nil {
			o.Error = r.Err.Error()
		} else {
			o.InstrumentID = r.Instrument.String()
			o.Kind = r.Identity.Kind
			o.Exchange = r.Identity.Exchange
			o.ProviderSymbol = r.Listing.Symbol()
		}
		out.Instruments[i] = o
	}
	return nil, out, nil
}

func (s *server) coverage(ctx context.Context, _ *mcp.CallToolRequest, in CoverageInput) (*mcp.CallToolResult, CoverageOutput, error) {
	interval, err := svcmarketdata.ParseInterval(in.Interval)
	if err != nil {
		return nil, CoverageOutput{}, err
	}
	rng, err := svcmarketdata.ParseRange(in.From, in.To)
	if err != nil {
		return nil, CoverageOutput{}, err
	}
	md, err := s.marketData(in.Provider)
	if err != nil {
		return nil, CoverageOutput{}, err
	}
	resp, err := md.DatasetsCoverage(ctx, svcmarketdata.DatasetsRequest{
		Instruments: instrumentRequests(in.Symbols), Interval: interval, Range: rng,
	})
	if err != nil {
		return nil, CoverageOutput{}, err
	}
	out := CoverageOutput{Provider: md.Provider(), Interval: interval.String(), Results: make([]SymbolCoverage, len(resp.Results))}
	for i, r := range resp.Results {
		out.Results[i] = symbolCoverage(r)
	}
	return nil, out, nil
}

func instrumentRequests(symbols []string) []svcmarketdata.InstrumentRequest {
	out := make([]svcmarketdata.InstrumentRequest, len(symbols))
	for i, sym := range symbols {
		out[i] = svcmarketdata.InstrumentRequest{Symbol: sym}
	}
	return out
}

func symbolCoverage(r svcmarketdata.CoverageResult) SymbolCoverage {
	out := SymbolCoverage{Symbol: r.Request.Symbol}
	if !r.Instrument.IsZero() {
		out.InstrumentID = r.Instrument.String()
	}
	if r.Err != nil {
		out.Error = r.Err.Error()
		return out
	}
	cov := r.Coverage
	if !cov.Range.Start().IsZero() {
		out.Range = &TimeSpan{Start: rfc3339(cov.Range.Start()), End: rfc3339(cov.Range.End())}
	}
	for _, p := range cov.Partitions {
		po := PartitionOutput{Month: fmt.Sprintf("%04d-%02d", p.Year, int(p.Month)), Status: p.Status.String()}
		if p.Manifest != nil {
			po.BarCount = p.Manifest.BarCount
		}
		out.Partitions = append(out.Partitions, po)
	}
	for _, g := range cov.Gaps {
		out.Gaps = append(out.Gaps, gapOutput(g))
	}
	out.Raw = dataSpanOutput(r.Inventory.Raw)
	out.Canonical = dataSpanOutput(r.Inventory.Canonical)
	return out
}

func gapOutput(g marketruntime.Gap) GapOutput {
	return GapOutput{State: g.State.String(), Start: rfc3339(g.Span.Start()), End: rfc3339(g.Span.End())}
}

func dataSpanOutput(span *marketruntime.DataSpan) *DataSpanOutput {
	if span == nil {
		return nil
	}
	return &DataSpanOutput{First: rfc3339(span.First), Last: rfc3339(span.Last), End: rfc3339(span.End), Partitions: span.Partitions}
}

// rfc3339 formats t in UTC, or "" for the zero time.
func rfc3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
