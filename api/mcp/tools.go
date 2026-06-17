package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/rustyeddy/trader/service"
)

// toolDef describes one MCP tool exposed to the client.
type toolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// tools returns all tools this server exposes. Write tools are included only
// when s.writeEnable is true.
func (s *Server) tools() []toolDef {
	all := []toolDef{
		{
			Name:        "get_account_summary",
			Description: "Return current account balance, NAV, margin, and unrealized P/L from OANDA.",
			InputSchema: schema(nil, nil),
		},
		{
			Name:        "list_open_trades",
			Description: "Return all open positions on the OANDA account.",
			InputSchema: schema(nil, nil),
		},
		{
			Name:        "get_transactions",
			Description: "Return OANDA account transactions with ID > since_id.",
			InputSchema: schema(map[string]any{
				"since_id": prop("integer", "Return transactions with ID greater than this value (0 = from start)"),
			}, nil),
		},
		{
			Name:        "run_backtest",
			Description: "Run one or more backtest configs and return the result summaries.",
			InputSchema: schema(map[string]any{
				"config_paths": prop("array", "List of backtest config path specs on the server (files, directories, or glob patterns)"),
			}, []string{"config_paths"}),
		},
		{
			Name:        "get_prices",
			Description: "Return live bid/ask prices and spread in pips for one or more instruments. Defaults to all major pairs.",
			InputSchema: schema(map[string]any{
				"instruments": prop("array", "Trader-format instrument symbols to query, e.g. [\"EURUSD\",\"GBPUSD\"]. Empty = all major pairs."),
			}, nil),
		},
		{
			Name:        "get_candles_csv",
			Description: "Return local candles for an instrument/timeframe/date-range in canonical CSV format.",
			InputSchema: schema(map[string]any{
				"instrument": prop("string", "Instrument, e.g. EURUSD or USD_JPY"),
				"timeframe":  prop("string", "Candle timeframe: M1, H1, or D1"),
				"from":       prop("string", "Start date inclusive, format YYYY-MM-DD"),
				"to":         prop("string", "Optional end date inclusive, format YYYY-MM-DD; defaults to now/latest available"),
				"source":     prop("string", "Optional local candle source override, default oanda"),
			}, []string{"instrument", "timeframe", "from"}),
		},
	}

	if s.writeEnable {
		all = append(all,
			toolDef{
				Name:        "download_candles",
				Description: "Download OANDA candles for an instrument/timeframe/date-range and write them to the local candle store.",
				InputSchema: schema(map[string]any{
					"instrument": prop("string", "OANDA-format instrument, e.g. EUR_USD or USD_JPY"),
					"timeframe":  prop("string", "Candle granularity: M1, H1, or D1"),
					"from":       prop("string", "Start date inclusive, format YYYY-MM-DD"),
					"to":         prop("string", "End date inclusive, format YYYY-MM-DD"),
					"raw_dir":    prop("string", "Optional root dir for raw bid+ask preservation (default /srv/trading/data/raw; empty string skips raw write)"),
				}, []string{"instrument", "timeframe", "from", "to"}),
			},
			toolDef{
				Name:        "place_order",
				Description: "Size and submit a risk-based market order. Set confirm=false to preview without submitting.",
				InputSchema: schema(map[string]any{
					"instrument": prop("string", "OANDA instrument e.g. EUR_USD, USD_JPY"),
					"side":       prop("string", "'long' or 'short'"),
					"risk_pct":   prop("number", "Percent of account NAV to risk (default 1.0)"),
					"stop_pips":  prop("number", "Stop distance in pips"),
					"confirm":    prop("boolean", "true to submit; false (default) to preview only"),
				}, []string{"instrument", "side", "stop_pips"}),
			},
			toolDef{
				Name:        "close_trade",
				Description: "Close an open trade fully or partially.",
				InputSchema: schema(map[string]any{
					"trade_id": prop("string", "OANDA trade ID"),
					"units":    prop("integer", "Units to close (0 = full close)"),
				}, []string{"trade_id"}),
			},
			toolDef{
				Name:        "update_stop",
				Description: "Update the stop-loss and/or take-profit on an open trade. Use 0 to leave unchanged, <0 to cancel.",
				InputSchema: schema(map[string]any{
					"trade_id":   prop("string", "OANDA trade ID"),
					"stop_price": prop("number", "New stop-loss price (0 = unchanged, <0 = cancel)"),
					"take_price": prop("number", "New take-profit price (0 = unchanged, <0 = cancel)"),
				}, []string{"trade_id"}),
			},
		)
	}

	return all
}

// handleToolsList returns the tool list in MCP format.
func (s *Server) handleToolsList() (any, *rpcError) {
	return map[string]any{"tools": s.tools()}, nil
}

// handleToolsCall dispatches a tools/call request.
func (s *Server) handleToolsCall(ctx context.Context, raw json.RawMessage) (any, *rpcError) {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &rpcError{Code: errInvalidParams, Message: "invalid tools/call params"}
	}

	if s.svc.OANDA == nil {
		switch p.Name {
		case "run_backtest", "get_candles_csv":
			// allowed without OANDA because they use local data only
		default:
			return errContent("OANDA not configured — start server with --token to enable live endpoints"), nil
		}
	}

	switch p.Name {
	case "get_account_summary":
		return s.toolGetAccountSummary(ctx)
	case "get_prices":
		return s.toolGetPrices(ctx, p.Arguments)
	case "list_open_trades":
		return s.toolListOpenTrades(ctx)
	case "get_transactions":
		return s.toolGetTransactions(ctx, p.Arguments)
	case "run_backtest":
		return s.toolRunBacktest(ctx, p.Arguments)
	case "get_candles_csv":
		return s.toolGetCandlesCSV(ctx, p.Arguments)
	case "download_candles":
		if !s.writeEnable {
			return errContent("download_candles requires --enable-write"), nil
		}
		return s.toolDownloadCandles(ctx, p.Arguments)
	case "place_order":
		if !s.writeEnable {
			return errContent("place_order requires --enable-write"), nil
		}
		return s.toolPlaceOrder(ctx, p.Arguments)
	case "close_trade":
		if !s.writeEnable {
			return errContent("close_trade requires --enable-write"), nil
		}
		return s.toolCloseTrade(ctx, p.Arguments)
	case "update_stop":
		if !s.writeEnable {
			return errContent("update_stop requires --enable-write"), nil
		}
		return s.toolUpdateStop(ctx, p.Arguments)
	default:
		return nil, &rpcError{Code: errMethodNotFound, Message: fmt.Sprintf("unknown tool: %s", p.Name)}
	}
}

// ── tool implementations ──────────────────────────────────────────────────

func (s *Server) toolGetAccountSummary(ctx context.Context) (any, *rpcError) {
	summary, err := s.svc.GetAccountSummary(ctx)
	if err != nil {
		return errContent(fmt.Sprintf("get_account_summary: %v", err)), nil
	}
	return jsonContent(summary), nil
}

func (s *Server) toolGetPrices(ctx context.Context, raw json.RawMessage) (any, *rpcError) {
	var args struct {
		Instruments []string `json:"instruments"`
	}
	if raw != nil {
		_ = json.Unmarshal(raw, &args)
	}
	prices, err := s.svc.GetPrices(ctx, service.GetPricesRequest{
		Instruments: args.Instruments,
	})
	if err != nil {
		return errContent(fmt.Sprintf("get_prices: %v", err)), nil
	}
	return jsonContent(map[string]any{"prices": prices}), nil
}

func (s *Server) toolListOpenTrades(ctx context.Context) (any, *rpcError) {
	trades, err := s.svc.ListOpenTrades(ctx)
	if err != nil {
		return errContent(fmt.Sprintf("list_open_trades: %v", err)), nil
	}
	return jsonContent(trades), nil
}

func (s *Server) toolGetTransactions(ctx context.Context, raw json.RawMessage) (any, *rpcError) {
	var args struct {
		SinceID int64 `json:"since_id"`
	}
	if raw != nil {
		_ = json.Unmarshal(raw, &args)
	}
	txns, lastID, err := s.svc.GetTransactions(ctx, args.SinceID)
	if err != nil {
		return errContent(fmt.Sprintf("get_transactions: %v", err)), nil
	}
	return jsonContent(map[string]any{
		"transactions":        txns,
		"last_transaction_id": lastID,
	}), nil
}

func (s *Server) toolRunBacktest(ctx context.Context, raw json.RawMessage) (any, *rpcError) {
	var args struct {
		ConfigPaths []string `json:"config_paths"`
	}
	if err := json.Unmarshal(raw, &args); err != nil || len(args.ConfigPaths) == 0 {
		return nil, &rpcError{Code: errInvalidParams, Message: "config_paths (array of strings) is required"}
	}

	summaries, err := s.svc.RunBacktestPathSpecsAndWriteReports(ctx, args.ConfigPaths, s.effectiveReportsDir())
	if err != nil {
		return errContent(fmt.Sprintf("run_backtest: %v", err)), nil
	}
	return jsonContent(map[string]any{
		"count":     len(summaries),
		"summaries": summaries,
	}), nil
}

func (s *Server) toolGetCandlesCSV(ctx context.Context, raw json.RawMessage) (any, *rpcError) {
	var args struct {
		Instrument string `json:"instrument"`
		Timeframe  string `json:"timeframe"`
		From       string `json:"from"`
		To         string `json:"to"`
		Source     string `json:"source"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, &rpcError{Code: errInvalidParams, Message: "invalid get_candles_csv args"}
	}
	if args.Instrument == "" || args.Timeframe == "" || args.From == "" {
		return nil, &rpcError{Code: errInvalidParams, Message: "instrument, timeframe, and from are required"}
	}

	result, err := s.svc.CandlesCSV(ctx, service.CandlesCSVRequest{
		Instrument: args.Instrument,
		Timeframe:  args.Timeframe,
		From:       args.From,
		To:         args.To,
		Source:     args.Source,
	})
	if err != nil {
		return errContent(fmt.Sprintf("get_candles_csv: %v", err)), nil
	}
	return map[string]any{
		"content": []map[string]any{{
			"type": "text",
			"text": result.CSV,
		}},
		"metadata": map[string]any{
			"instrument": result.Instrument,
			"timeframe":  result.Timeframe,
			"from":       result.From,
			"to":         result.To,
			"source":     result.Source,
			"count":      result.Count,
			"mime_type":  "text/csv",
		},
	}, nil
}

func (s *Server) toolPlaceOrder(ctx context.Context, raw json.RawMessage) (any, *rpcError) {
	var args struct {
		Instrument string  `json:"instrument"`
		Side       string  `json:"side"`
		RiskPct    float64 `json:"risk_pct"`
		StopPips   float64 `json:"stop_pips"`
		StopPrice  float64 `json:"stop_price"`
		Units      int64   `json:"units"`
		Confirm    bool    `json:"confirm"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, &rpcError{Code: errInvalidParams, Message: "invalid place_order args"}
	}
	if args.Instrument == "" || args.Side == "" {
		return nil, &rpcError{Code: errInvalidParams, Message: "instrument and side are required"}
	}
	riskPct := args.RiskPct
	if riskPct == 0 {
		riskPct = 1.0
	}
	result, err := s.svc.PlaceMarketOrder(ctx, service.PlaceMarketOrderRequest{
		Instrument: args.Instrument,
		Side:       strings.ToLower(args.Side),
		RiskPct:    riskPct,
		StopPips:   args.StopPips,
		StopPrice:  args.StopPrice,
		Units:      args.Units,
		Confirm:    args.Confirm,
	})
	if err != nil {
		return errContent(fmt.Sprintf("place_order: %v", err)), nil
	}
	return jsonContent(result), nil
}

func (s *Server) toolCloseTrade(ctx context.Context, raw json.RawMessage) (any, *rpcError) {
	var args struct {
		TradeID string `json:"trade_id"`
		Units   int64  `json:"units"`
	}
	if err := json.Unmarshal(raw, &args); err != nil || args.TradeID == "" {
		return nil, &rpcError{Code: errInvalidParams, Message: "trade_id is required"}
	}
	result, err := s.svc.CloseTrade(ctx, args.TradeID, args.Units)
	if err != nil {
		return errContent(fmt.Sprintf("close_trade: %v", err)), nil
	}
	return jsonContent(result), nil
}

func (s *Server) toolUpdateStop(ctx context.Context, raw json.RawMessage) (any, *rpcError) {
	var args struct {
		TradeID   string  `json:"trade_id"`
		StopPrice float64 `json:"stop_price"`
		TakePrice float64 `json:"take_price"`
	}
	if err := json.Unmarshal(raw, &args); err != nil || args.TradeID == "" {
		return nil, &rpcError{Code: errInvalidParams, Message: "trade_id is required"}
	}
	if err := s.svc.UpdateTradeStop(ctx, args.TradeID, args.StopPrice, args.TakePrice); err != nil {
		return errContent(fmt.Sprintf("update_stop: %v", err)), nil
	}
	return textContent(fmt.Sprintf("stop updated for trade %s", args.TradeID)), nil
}

func (s *Server) toolDownloadCandles(ctx context.Context, raw json.RawMessage) (any, *rpcError) {
	var args struct {
		Instrument string `json:"instrument"`
		Timeframe  string `json:"timeframe"`
		From       string `json:"from"`
		To         string `json:"to"`
		RawDir     string `json:"raw_dir"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, &rpcError{Code: errInvalidParams, Message: "invalid download_candles args"}
	}
	if args.Instrument == "" || args.Timeframe == "" || args.From == "" || args.To == "" {
		return nil, &rpcError{Code: errInvalidParams, Message: "instrument, timeframe, from, and to are required"}
	}
	from, err := time.Parse("2006-01-02", args.From)
	if err != nil {
		return errContent(fmt.Sprintf("bad from date %q: %v", args.From, err)), nil
	}
	to, err := time.Parse("2006-01-02", args.To)
	if err != nil {
		return errContent(fmt.Sprintf("bad to date %q: %v", args.To, err)), nil
	}
	rawDir := args.RawDir
	if rawDir == "" {
		rawDir = "/srv/trading/data/raw"
	}

	var progress []string
	result, err := s.svc.DownloadOandaCandles(ctx, service.DownloadOandaCandlesRequest{
		Instrument: args.Instrument,
		Timeframe:  args.Timeframe,
		From:       from,
		To:         to,
		RawDir:     rawDir,
		OnProgress: func(line string) { progress = append(progress, line) },
	})
	if err != nil {
		return errContent(fmt.Sprintf("download_candles: %v", err)), nil
	}
	return jsonContent(map[string]any{
		"months_processed": result.MonthsProcessed,
		"candles_written":  result.CandlesWritten,
		"progress":         progress,
	}), nil
}

// ── schema helpers ────────────────────────────────────────────────────────

func schema(properties map[string]any, required []string) map[string]any {
	s := map[string]any{"type": "object"}
	if len(properties) > 0 {
		s["properties"] = properties
	}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func prop(typ, description string) map[string]any {
	m := map[string]any{"type": typ, "description": description}
	if typ == "array" {
		m["items"] = map[string]any{"type": "string"}
	}
	return m
}
