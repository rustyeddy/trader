package mcpserver

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	svcmarketdata "github.com/rustyeddy/trader/internal/service/marketdata"
)

// The data-mutating market-data tools (#432): canonicalize and update.
// Both require write access (requireWrites), are thin adapters over one
// MarketData operation each, and report a result per symbol plus a
// summary. Per-symbol failures are never call errors: the call fails only
// for an invalid request, a disabled write policy, or no market data.
// With a progress token, each finished symbol sends a progress
// notification, so a client doesn't time out on a long build or download.

// CanonicalizeInput is trader_marketdata_canonicalize's request.
type CanonicalizeInput struct {
	Symbols  []string `json:"symbols" jsonschema:"symbols to canonicalize, for example EURUSD or SPY"`
	Provider string   `json:"provider,omitempty" jsonschema:"market-data provider (oanda, alpaca, or stooq); defaults to the server's provider"`
	Interval string   `json:"interval" jsonschema:"bar interval: M1, H1, H4, D1, or W1"`
	From     string   `json:"from,omitempty" jsonschema:"range start, YYYY-MM-DD or RFC3339; give with to, or omit both for each symbol's whole source"`
	To       string   `json:"to,omitempty" jsonschema:"range end (exclusive), YYYY-MM-DD or RFC3339"`
	Force    bool     `json:"force,omitempty" jsonschema:"rebuild partitions that are already current"`
}

// UpdateInput is trader_marketdata_update's request.
type UpdateInput struct {
	Symbols  []string `json:"symbols" jsonschema:"symbols to update, for example EURUSD or SPY"`
	Provider string   `json:"provider,omitempty" jsonschema:"market-data provider (oanda, alpaca, or stooq); defaults to the server's provider"`
	Interval string   `json:"interval" jsonschema:"bar interval: M1, H1, H4, D1, or W1"`
	From     string   `json:"from,omitempty" jsonschema:"range start, YYYY-MM-DD or RFC3339; give with to, or omit both to update from each symbol's last canonical bar through now"`
	To       string   `json:"to,omitempty" jsonschema:"range end (exclusive), YYYY-MM-DD or RFC3339"`
}

// DatasetsOutput is canonicalize's and update's result.
type DatasetsOutput struct {
	Provider  string `json:"provider"`
	Interval  string `json:"interval"`
	Operation string `json:"operation" jsonschema:"canonicalize or update"`
	// OK is true when no symbol failed.
	OK      bool            `json:"ok"`
	Summary DatasetsSummary `json:"summary"`
	Results []DatasetOutput `json:"results"`
}

// DatasetsSummary counts results by status.
type DatasetsSummary struct {
	Built   int `json:"built"`
	Updated int `json:"updated"`
	Current int `json:"current"`
	Failed  int `json:"failed"`
}

// DatasetOutput is one symbol's outcome.
type DatasetOutput struct {
	Symbol       string `json:"symbol"`
	InstrumentID string `json:"instrument_id,omitempty"`
	Status       string `json:"status" jsonschema:"built, updated, current, or failed"`
	// Source is where the data came from: "archive" (a stooq native
	// archive, nothing fetched from the network), "raw" (raw data already
	// held), or "provider" (an update that may download).
	Source string `json:"source,omitempty" jsonschema:"archive, raw, or provider"`
	// Range is the range acted on.
	Range *TimeSpan `json:"range,omitempty"`
	// Archive describes the stooq archive read, when Source is archive.
	Archive *ArchiveOutput `json:"archive,omitempty"`
	// DownloadedPartitions counts raw partitions an update downloaded.
	DownloadedPartitions int             `json:"downloaded_partitions,omitempty"`
	PublishedPartitions  int             `json:"published_partitions"`
	PublishedBars        int             `json:"published_bars"`
	Raw                  *DataSpanOutput `json:"raw,omitempty"`
	CanonicalBefore      *DataSpanOutput `json:"canonical_before,omitempty"`
	CanonicalAfter       *DataSpanOutput `json:"canonical_after,omitempty"`
	Error                string          `json:"error,omitempty"`
}

// ArchiveOutput summarizes the rows a stooq archive held for the symbol.
type ArchiveOutput struct {
	FirstDate string `json:"first_date"`
	LastDate  string `json:"last_date"`
	Rows      int    `json:"rows"`
}

func (s *server) registerMarketDataWrites(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: "trader_marketdata_canonicalize",
		Description: "Build canonical market data for symbols from provider-native data already held: a stooq archive under the " +
			"server's archive root, or raw data in the raw store. Never downloads. Omit from/to for each symbol's whole source. " +
			"Requires write access (--allow-writes). One result per symbol, plus a summary.",
	}, s.canonicalize)
	mcp.AddTool(srv, &mcp.Tool{
		Name: "trader_marketdata_update",
		Description: "Bring canonical market data for symbols forward to the latest provider data: oanda and alpaca download " +
			"new raw data; stooq has no live feed and re-reads its archive. Omit from/to to update from each symbol's last " +
			"canonical bar through now. Requires write access (--allow-writes). One result per symbol, plus a summary.",
	}, s.update)
}

func (s *server) canonicalize(ctx context.Context, req *mcp.CallToolRequest, in CanonicalizeInput) (*mcp.CallToolResult, DatasetsOutput, error) {
	const tool, what = "trader_marketdata_canonicalize", "canonicalization"
	md, dreq, err := s.datasetsRequest(ctx, tool, what, in.Provider, in.Symbols, in.Interval, in.From, in.To)
	if err != nil {
		return nil, DatasetsOutput{}, err
	}
	dreq.OnResult = s.progress(ctx, req, len(in.Symbols))
	resp, err := md.CanonicalizeDatasets(ctx, dreq, svcmarketdata.CanonicalizeOptions{Force: in.Force})
	return s.datasetsOutput(ctx, tool, what, "canonicalize", md, dreq, resp, err)
}

func (s *server) update(ctx context.Context, req *mcp.CallToolRequest, in UpdateInput) (*mcp.CallToolResult, DatasetsOutput, error) {
	const tool, what = "trader_marketdata_update", "update"
	md, dreq, err := s.datasetsRequest(ctx, tool, what, in.Provider, in.Symbols, in.Interval, in.From, in.To)
	if err != nil {
		return nil, DatasetsOutput{}, err
	}
	dreq.OnResult = s.progress(ctx, req, len(in.Symbols))
	resp, err := md.UpdateDatasets(ctx, dreq)
	return s.datasetsOutput(ctx, tool, what, "update", md, dreq, resp, err)
}

// datasetsRequest checks write access and parses a mutating tool's
// request; every error it returns is already client-safe.
func (s *server) datasetsRequest(ctx context.Context, tool, what, provider string, symbols []string, interval, from, to string) (MarketData, svcmarketdata.DatasetsRequest, error) {
	if err := s.requireWrites(tool); err != nil {
		return nil, svcmarketdata.DatasetsRequest{}, err
	}
	iv, err := svcmarketdata.ParseInterval(interval)
	if err != nil {
		return nil, svcmarketdata.DatasetsRequest{}, s.publicError(ctx, tool, what, err)
	}
	rng, err := svcmarketdata.ParseRange(from, to)
	if err != nil {
		return nil, svcmarketdata.DatasetsRequest{}, s.publicError(ctx, tool, what, err)
	}
	md, err := s.marketData(provider)
	if err != nil {
		return nil, svcmarketdata.DatasetsRequest{}, s.publicError(ctx, tool, what, err)
	}
	return md, svcmarketdata.DatasetsRequest{Instruments: instrumentRequests(symbols), Interval: iv, Range: rng}, nil
}

// progress returns an OnResult that sends a progress notification per
// finished symbol, or nil when the client asked for no progress.
func (s *server) progress(ctx context.Context, req *mcp.CallToolRequest, total int) func(svcmarketdata.DatasetResult) {
	if req == nil || req.Params == nil || req.Session == nil {
		return nil
	}
	token := req.Params.GetProgressToken()
	if token == nil {
		return nil
	}
	done := 0
	return func(res svcmarketdata.DatasetResult) {
		done++
		err := req.Session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{
			ProgressToken: token,
			Progress:      float64(done),
			Total:         float64(total),
			Message:       fmt.Sprintf("%s: %s", res.Request.Symbol, res.Status),
		})
		if err != nil {
			s.deps.Logger.DebugContext(ctx, "mcp progress notification failed", "error", err)
		}
	}
}

// datasetsOutput translates a mutating operation's response. A call error
// with results (cancellation partway) still fails the call; its results
// were reported through progress and are logged.
func (s *server) datasetsOutput(ctx context.Context, tool, what, op string, md MarketData, req svcmarketdata.DatasetsRequest, resp svcmarketdata.DatasetsResponse, err error) (*mcp.CallToolResult, DatasetsOutput, error) {
	if err != nil {
		return nil, DatasetsOutput{}, s.publicError(ctx, tool, what, err)
	}
	out := DatasetsOutput{Provider: md.Provider(), Interval: req.Interval.String(), Operation: op, OK: true,
		Results: make([]DatasetOutput, len(resp.Results))}
	for i, r := range resp.Results {
		o := s.datasetOutput(ctx, tool, what, r)
		switch r.Status {
		case svcmarketdata.DatasetBuilt:
			out.Summary.Built++
		case svcmarketdata.DatasetUpdated:
			out.Summary.Updated++
		case svcmarketdata.DatasetCurrent:
			out.Summary.Current++
		case svcmarketdata.DatasetFailed:
			out.Summary.Failed++
			out.OK = false
		}
		out.Results[i] = o
	}
	return nil, out, nil
}

func (s *server) datasetOutput(ctx context.Context, tool, what string, r svcmarketdata.DatasetResult) DatasetOutput {
	o := DatasetOutput{
		Symbol:              r.Request.Symbol,
		Status:              string(r.Status),
		PublishedPartitions: r.PublishedPartitions,
		PublishedBars:       r.PublishedBars,
		Raw:                 dataSpanOutput(r.Raw),
		CanonicalBefore:     dataSpanOutput(r.CanonicalBefore),
		CanonicalAfter:      dataSpanOutput(r.CanonicalAfter),
	}
	if !r.Instrument.IsZero() {
		o.InstrumentID = r.Instrument.String()
	}
	if !r.Range.Start().IsZero() {
		o.Range = &TimeSpan{Start: rfc3339(r.Range.Start()), End: rfc3339(r.Range.End())}
	}
	switch {
	case r.Convert != nil:
		o.Source = "archive"
		imp := r.Convert.Import
		if imp.RowsImported > 0 {
			o.Archive = &ArchiveOutput{FirstDate: date(imp.FirstDate), LastDate: date(imp.LastDate), Rows: imp.RowsImported}
		}
	case r.Build != nil:
		o.Source = "raw"
	case r.Update != nil:
		o.Source = "provider"
		o.DownloadedPartitions = len(r.Update.Sync.Result.Downloaded)
	}
	if r.Err != nil {
		o.Error = s.publicMessage(ctx, tool, r.Request.Symbol, what, r.Err)
	}
	return o
}

func date(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format("2006-01-02")
}
