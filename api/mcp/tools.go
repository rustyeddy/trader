package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/rustyeddy/trader/config"
	"github.com/rustyeddy/trader/market"
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
			Name:        "list_accounts",
			Description: "List the OANDA accounts the configured token can access, flagging the default (first) account.",
			InputSchema: schema(nil, nil),
		},
		{
			Name:        "get_account_summary",
			Description: "Return current account balance, NAV, margin, and unrealized P/L from OANDA.",
			InputSchema: schema(map[string]any{
				"account_id": propAccountReadOptional,
			}, nil),
		},
		{
			Name:        "list_open_trades",
			Description: "Return all open positions on the OANDA account.",
			InputSchema: schema(map[string]any{
				"account_id": propAccountReadOptional,
			}, nil),
		},
		{
			Name:        "get_transactions",
			Description: "Return OANDA account transactions with ID > since_id.",
			InputSchema: schema(map[string]any{
				"account_id": propAccountReadOptional,
				"since_id":   prop("integer", "Return transactions with ID greater than this value (0 = from start)"),
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
				"account_id":  propAccountReadOptional,
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
		{
			Name:        "get_candle_stats",
			Description: "Analyse a local candle dataset and return swing range, spread, trend, and session statistics.",
			InputSchema: schema(map[string]any{
				"instrument": prop("string", "Instrument, e.g. EURUSD"),
				"timeframe":  prop("string", "Candle timeframe: M1, H1, or D1 (default H1)"),
				"from":       prop("string", "Start date inclusive, format YYYY-MM-DD"),
				"to":         prop("string", "End date inclusive, format YYYY-MM-DD"),
				"source":     prop("string", "Optional data source override (default oanda)"),
				"units":      prop("integer", "Position size for USD column (e.g. 100000 = standard lot; 0 = omit)"),
			}, []string{"instrument", "from", "to"}),
		},
		{
			Name:        "validate_candles",
			Description: "Scan local candle months for missing expected bars and raw-source mismatches. Returns a validation report.",
			InputSchema: schema(map[string]any{
				"instruments": prop("array", "Instruments to validate, e.g. [\"EURUSD\",\"GBPUSD\"]"),
				"from":        prop("string", "Start month inclusive, format YYYY-MM"),
				"to":          prop("string", "End month inclusive, format YYYY-MM"),
				"timeframe":   prop("string", "Candle timeframe to validate: M1, H1, or D1 (default H1)"),
				"source":      prop("string", "Stored candle source to validate (default oanda)"),
			}, []string{"instruments", "from", "to"}),
		},
		{
			Name:        "get_pip_values",
			Description: "Return the USD pip value for 1/10/100/1000 pips per major FX pair at a given position size.",
			InputSchema: schema(map[string]any{
				"units":       prop("integer", "Position size in units (default 100000 = standard lot)"),
				"instruments": prop("array", "Subset of instruments, e.g. [\"EURUSD\",\"USDJPY\"]. Empty = all major pairs."),
			}, nil),
		},
		{
			Name:        "get_position",
			Description: "Calculate notional value, margin required, and pip P&L for a position. Omit units and notional to get a micro/mini/standard lot table.",
			InputSchema: schema(map[string]any{
				"instrument": prop("string", "FX pair, e.g. EURUSD"),
				"price":      prop("number", "Mid price (fetched live from OANDA if omitted and OANDA is configured)"),
				"units":      prop("integer", "Specific position size; omit for standard table"),
				"notional":   prop("number", "Target USD notional; converted to units (use instead of units)"),
				"pips":       prop("number", "Include USD value of this many pips in the result"),
			}, []string{"instrument"}),
		},
	}

	// Infra — always available, no OANDA required.
	all = append(all,
		toolDef{
			Name:        "get_version",
			Description: "Return the trader server version string.",
			InputSchema: schema(nil, nil),
		},
		toolDef{
			Name:        "get_health",
			Description: "Return the server health status.",
			InputSchema: schema(nil, nil),
		},
	)

	// Bot management — read tools are always available; write tools gated.
	all = append(all,
		toolDef{
			Name:        "list_bots",
			Description: "List live strategy bots (running and stopped) on an account; defaults to the first/default account.",
			InputSchema: schema(map[string]any{
				"account_id": propAccountReadOptional,
			}, nil),
		},
		toolDef{
			Name:        "get_bot",
			Description: "Return the status of a single live strategy bot by ID.",
			InputSchema: schema(map[string]any{
				"id": prop("string", "Bot ID returned by start_bot or list_bots"),
			}, []string{"id"}),
		},
	)

	if s.writeEnable {
		all = append(all,
			toolDef{
				Name:        "start_bot",
				Description: "Start a new live strategy bot on the server.",
				InputSchema: schema(map[string]any{
					"account_id":    propAccountWriteRequired,
					"instrument":    prop("string", "OANDA-format instrument, e.g. EUR_USD"),
					"strategy":      prop("string", "Strategy kind, e.g. donchian-v6, ema-cross"),
					"tick_interval": prop("string", "How often the strategy ticks, e.g. 60s, 5m (default 60s)"),
					"risk_pct":      prop("number", "Percent of account NAV to risk per trade (default 1.0)"),
					"max_units":     prop("integer", "Maximum position size in units (0 = no limit)"),
				}, []string{"account_id", "instrument", "strategy"}),
			},
			toolDef{
				Name:        "stop_bot",
				Description: "Stop a running live strategy bot by ID.",
				InputSchema: schema(map[string]any{
					"id": prop("string", "Bot ID to stop"),
				}, []string{"id"}),
			},
		)
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
					"account_id": propAccountWriteRequired,
					"instrument": prop("string", "OANDA instrument e.g. EUR_USD, USD_JPY"),
					"side":       prop("string", "'long' or 'short'"),
					"risk_pct":   prop("number", "Percent of account NAV to risk (default 1.0)"),
					"stop_pips":  prop("number", "Stop distance in pips"),
					"confirm":    prop("boolean", "true to submit; false (default) to preview only"),
				}, []string{"account_id", "instrument", "side", "stop_pips"}),
			},
			toolDef{
				Name:        "close_trade",
				Description: "Close an open trade fully or partially.",
				InputSchema: schema(map[string]any{
					"account_id": propAccountWriteRequired,
					"trade_id":   prop("string", "OANDA trade ID"),
					"units":      prop("integer", "Units to close (0 = full close)"),
				}, []string{"account_id", "trade_id"}),
			},
			toolDef{
				Name:        "update_stop",
				Description: "Update the stop-loss and/or take-profit on an open trade. Use 0 to leave unchanged, <0 to cancel.",
				InputSchema: schema(map[string]any{
					"account_id": propAccountWriteRequired,
					"trade_id":   prop("string", "OANDA trade ID"),
					"stop_price": prop("number", "New stop-loss price (0 = unchanged, <0 = cancel)"),
					"take_price": prop("number", "New take-profit price (0 = unchanged, <0 = cancel)"),
				}, []string{"account_id", "trade_id"}),
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
		case "run_backtest", "get_candles_csv", "get_candle_stats", "validate_candles",
			"get_pip_values", "get_position", "list_bots", "get_bot", "stop_bot",
			"get_version", "get_health":
			// allowed without OANDA — local data, pure calculation, or bot management
		default:
			return errContent("OANDA not configured — start server with --token to enable live endpoints"), nil
		}
	}

	switch p.Name {
	case "list_accounts":
		return s.toolListAccounts(ctx)
	case "get_account_summary":
		return s.toolGetAccountSummary(ctx, p.Arguments)
	case "get_prices":
		return s.toolGetPrices(ctx, p.Arguments)
	case "list_open_trades":
		return s.toolListOpenTrades(ctx, p.Arguments)
	case "get_transactions":
		return s.toolGetTransactions(ctx, p.Arguments)
	case "run_backtest":
		return s.toolRunBacktest(ctx, p.Arguments)
	case "get_candles_csv":
		return s.toolGetCandlesCSV(ctx, p.Arguments)
	case "get_candle_stats":
		return s.toolGetDataStats(ctx, p.Arguments)
	case "validate_candles":
		return s.toolValidateCandles(ctx, p.Arguments)
	case "get_pip_values":
		return s.toolGetPipValues(ctx, p.Arguments)
	case "get_position":
		return s.toolGetPosition(ctx, p.Arguments)
	case "get_version":
		return s.toolGetVersion()
	case "get_health":
		return s.toolGetHealth()
	case "list_bots":
		return s.toolListBots(ctx, p.Arguments)
	case "get_bot":
		return s.toolGetBot(p.Arguments)
	case "start_bot":
		if !s.writeEnable {
			return errContent("start_bot requires --enable-write"), nil
		}
		return s.toolStartBot(ctx, p.Arguments)
	case "stop_bot":
		if !s.writeEnable {
			return errContent("stop_bot requires --enable-write"), nil
		}
		return s.toolStopBot(p.Arguments)
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

// readAccount resolves the account for a read tool: the named account, or the
// first/default account when id is empty.
func (s *Server) readAccount(ctx context.Context, id string) (*service.Account, error) {
	if id == "" {
		return s.svc.FirstAccount(ctx)
	}
	return s.svc.Account(ctx, id)
}

// writeAccount resolves the account for a mutating tool. Mutations must name
// the account explicitly — an empty account_id is an error, never a default.
func (s *Server) writeAccount(ctx context.Context, id string) (*service.Account, *rpcError) {
	if id == "" {
		return nil, &rpcError{Code: errInvalidParams, Message: "account_id is required for write operations"}
	}
	acc, err := s.svc.Account(ctx, id)
	if err != nil {
		return nil, &rpcError{Code: errInvalidParams, Message: fmt.Sprintf("resolve account: %v", err)}
	}
	return acc, nil
}

func (s *Server) toolListAccounts(ctx context.Context) (any, *rpcError) {
	accts, err := s.svc.Accounts(ctx)
	if err != nil {
		return errContent(fmt.Sprintf("list_accounts: %v", err)), nil
	}
	def, _ := s.svc.FirstAccount(ctx)
	out := make([]map[string]any, 0, len(accts))
	for _, a := range accts {
		out = append(out, map[string]any{
			"id":         a.ID,
			"is_default": def != nil && a.ID == def.ID,
		})
	}
	return jsonContent(map[string]any{"accounts": out}), nil
}

func (s *Server) toolGetAccountSummary(ctx context.Context, raw json.RawMessage) (any, *rpcError) {
	acc, err := s.readAccount(ctx, parseAccountID(raw))
	if err != nil {
		return errContent(fmt.Sprintf("get_account_summary: %v", err)), nil
	}
	summary, err := acc.GetAccountSummary(ctx)
	if err != nil {
		return errContent(fmt.Sprintf("get_account_summary: %v", err)), nil
	}
	return jsonContent(summary), nil
}

func (s *Server) toolGetPrices(ctx context.Context, raw json.RawMessage) (any, *rpcError) {
	var args struct {
		AccountID   string   `json:"account_id"`
		Instruments []string `json:"instruments"`
	}
	if raw != nil {
		_ = json.Unmarshal(raw, &args)
	}
	acc, err := s.readAccount(ctx, args.AccountID)
	if err != nil {
		return errContent(fmt.Sprintf("get_prices: %v", err)), nil
	}
	prices, err := acc.GetPrices(ctx, service.GetPricesRequest{
		Instruments: args.Instruments,
	})
	if err != nil {
		return errContent(fmt.Sprintf("get_prices: %v", err)), nil
	}
	return jsonContent(map[string]any{"prices": prices}), nil
}

func (s *Server) toolListOpenTrades(ctx context.Context, raw json.RawMessage) (any, *rpcError) {
	acc, err := s.readAccount(ctx, parseAccountID(raw))
	if err != nil {
		return errContent(fmt.Sprintf("list_open_trades: %v", err)), nil
	}
	trades, err := acc.ListOpenTrades(ctx)
	if err != nil {
		return errContent(fmt.Sprintf("list_open_trades: %v", err)), nil
	}
	return jsonContent(trades), nil
}

func (s *Server) toolGetTransactions(ctx context.Context, raw json.RawMessage) (any, *rpcError) {
	var args struct {
		AccountID string `json:"account_id"`
		SinceID   int64  `json:"since_id"`
	}
	if raw != nil {
		_ = json.Unmarshal(raw, &args)
	}
	acc, err := s.readAccount(ctx, args.AccountID)
	if err != nil {
		return errContent(fmt.Sprintf("get_transactions: %v", err)), nil
	}
	txns, lastID, err := acc.GetTransactions(ctx, args.SinceID)
	if err != nil {
		return errContent(fmt.Sprintf("get_transactions: %v", err)), nil
	}
	return jsonContent(map[string]any{
		"transactions":        txns,
		"last_transaction_id": lastID,
	}), nil
}

// parseAccountID extracts the optional account_id field from raw arguments.
func parseAccountID(raw json.RawMessage) string {
	var a struct {
		AccountID string `json:"account_id"`
	}
	if raw != nil {
		_ = json.Unmarshal(raw, &a)
	}
	return a.AccountID
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
		AccountID  string  `json:"account_id"`
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
	acc, rerr := s.writeAccount(ctx, args.AccountID)
	if rerr != nil {
		return nil, rerr
	}
	riskPct := args.RiskPct
	if riskPct == 0 {
		riskPct = 1.0
	}
	result, err := acc.PlaceMarketOrder(ctx, service.PlaceMarketOrderRequest{
		Instrument: args.Instrument,
		Side:       strings.ToLower(args.Side),
		RiskPct:    market.RateFromFloat(riskPct / 100.0),
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
		AccountID string `json:"account_id"`
		TradeID   string `json:"trade_id"`
		Units     int64  `json:"units"`
	}
	if err := json.Unmarshal(raw, &args); err != nil || args.TradeID == "" {
		return nil, &rpcError{Code: errInvalidParams, Message: "trade_id is required"}
	}
	acc, rerr := s.writeAccount(ctx, args.AccountID)
	if rerr != nil {
		return nil, rerr
	}
	result, err := acc.CloseTrade(ctx, args.TradeID, args.Units)
	if err != nil {
		return errContent(fmt.Sprintf("close_trade: %v", err)), nil
	}
	return jsonContent(result), nil
}

func (s *Server) toolUpdateStop(ctx context.Context, raw json.RawMessage) (any, *rpcError) {
	var args struct {
		AccountID string  `json:"account_id"`
		TradeID   string  `json:"trade_id"`
		StopPrice float64 `json:"stop_price"`
		TakePrice float64 `json:"take_price"`
	}
	if err := json.Unmarshal(raw, &args); err != nil || args.TradeID == "" {
		return nil, &rpcError{Code: errInvalidParams, Message: "trade_id is required"}
	}
	acc, rerr := s.writeAccount(ctx, args.AccountID)
	if rerr != nil {
		return nil, rerr
	}
	if err := acc.UpdateTradeStop(ctx, args.TradeID, args.StopPrice, args.TakePrice); err != nil {
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

func (s *Server) toolGetDataStats(ctx context.Context, raw json.RawMessage) (any, *rpcError) {
	var args struct {
		Instrument string `json:"instrument"`
		Timeframe  string `json:"timeframe"`
		From       string `json:"from"`
		To         string `json:"to"`
		Source     string `json:"source"`
		Units      int64  `json:"units"`
	}
	if raw != nil {
		_ = json.Unmarshal(raw, &args)
	}
	if args.Instrument == "" || args.From == "" || args.To == "" {
		return nil, &rpcError{Code: errInvalidParams, Message: "instrument, from, and to are required"}
	}
	result, err := s.svc.DataStats(ctx, service.DataStatsRequest{
		Instrument: args.Instrument,
		Timeframe:  args.Timeframe,
		From:       args.From,
		To:         args.To,
		Source:     args.Source,
		Units:      args.Units,
	})
	if err != nil {
		return errContent(fmt.Sprintf("get_candle_stats: %v", err)), nil
	}
	return jsonContent(result), nil
}

func (s *Server) toolValidateCandles(ctx context.Context, raw json.RawMessage) (any, *rpcError) {
	var args struct {
		Instruments []string `json:"instruments"`
		From        string   `json:"from"`
		To          string   `json:"to"`
		Timeframe   string   `json:"timeframe"`
		Source      string   `json:"source"`
	}
	if raw != nil {
		_ = json.Unmarshal(raw, &args)
	}
	if len(args.Instruments) == 0 || args.From == "" || args.To == "" {
		return nil, &rpcError{Code: errInvalidParams, Message: "instruments, from, and to are required"}
	}
	from, err := time.Parse("2006-01", args.From)
	if err != nil {
		return nil, &rpcError{Code: errInvalidParams, Message: "from must be YYYY-MM"}
	}
	toMonth, err := time.Parse("2006-01", args.To)
	if err != nil {
		return nil, &rpcError{Code: errInvalidParams, Message: "to must be YYYY-MM"}
	}
	tf := args.Timeframe
	if tf == "" {
		tf = "H1"
	}
	src := args.Source
	if src == "" {
		src = "oanda"
	}
	report, err := s.svc.ValidateCandleData(ctx, service.ValidateCandleDataRequest{
		Instruments: args.Instruments,
		Source:      src,
		Timeframe:   tf,
		From:        from,
		To:          toMonth.AddDate(0, 1, 0),
	})
	if err != nil {
		return errContent(fmt.Sprintf("validate_candles: %v", err)), nil
	}
	return jsonContent(report), nil
}

func (s *Server) toolGetPipValues(ctx context.Context, raw json.RawMessage) (any, *rpcError) {
	var args struct {
		Units       int64    `json:"units"`
		Instruments []string `json:"instruments"`
	}
	if raw != nil {
		_ = json.Unmarshal(raw, &args)
	}
	result, err := s.svc.PipValues(ctx, service.PipValuesRequest{
		Units:       args.Units,
		Instruments: args.Instruments,
	})
	if err != nil {
		return errContent(fmt.Sprintf("get_pip_values: %v", err)), nil
	}
	return jsonContent(result), nil
}

func (s *Server) toolGetPosition(ctx context.Context, raw json.RawMessage) (any, *rpcError) {
	var args struct {
		Instrument string  `json:"instrument"`
		Price      float64 `json:"price"`
		Units      int64   `json:"units"`
		Notional   float64 `json:"notional"`
		Pips       float64 `json:"pips"`
	}
	if raw != nil {
		_ = json.Unmarshal(raw, &args)
	}
	if args.Instrument == "" {
		return nil, &rpcError{Code: errInvalidParams, Message: "instrument is required"}
	}
	result, err := s.svc.PositionCalc(ctx, service.PositionCalcRequest{
		Instrument: args.Instrument,
		Price:      args.Price,
		Units:      args.Units,
		Notional:   args.Notional,
		Pips:       args.Pips,
	})
	if err != nil {
		return errContent(fmt.Sprintf("get_position: %v", err)), nil
	}
	return jsonContent(result), nil
}

// ── bot tools ─────────────────────────────────────────────────────────────

// toolListBots lists bots for a specific account when account_id is given,
// otherwise every bot the server manages across all accounts. The all-accounts
// view reads in-process state and needs no OANDA connection.
func (s *Server) toolListBots(ctx context.Context, raw json.RawMessage) (any, *rpcError) {
	var bots []service.BotStatus
	if id := parseAccountID(raw); id != "" {
		acc, err := s.svc.Account(ctx, id)
		if err != nil {
			return errContent(fmt.Sprintf("list_bots: %v", err)), nil
		}
		bots = acc.ListBots()
	} else {
		bots = s.svc.ListBots()
	}
	return jsonContent(map[string]any{
		"count": len(bots),
		"bots":  bots,
	}), nil
}

func (s *Server) toolGetBot(raw json.RawMessage) (any, *rpcError) {
	var args struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &args); err != nil || args.ID == "" {
		return nil, &rpcError{Code: errInvalidParams, Message: "id is required"}
	}
	status, err := s.svc.GetBot(args.ID)
	if err != nil {
		return errContent(fmt.Sprintf("get_bot: %v", err)), nil
	}
	return jsonContent(status), nil
}

func (s *Server) toolStartBot(ctx context.Context, raw json.RawMessage) (any, *rpcError) {
	var args struct {
		AccountID    string  `json:"account_id"`
		Instrument   string  `json:"instrument"`
		Strategy     string  `json:"strategy"`
		TickInterval string  `json:"tick_interval"`
		RiskPct      float64 `json:"risk_pct"`
		MaxUnits     int64   `json:"max_units"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, &rpcError{Code: errInvalidParams, Message: "invalid start_bot args"}
	}
	if args.Instrument == "" || args.Strategy == "" {
		return nil, &rpcError{Code: errInvalidParams, Message: "instrument and strategy are required"}
	}
	acc, rerr := s.writeAccount(ctx, args.AccountID)
	if rerr != nil {
		return nil, rerr
	}
	riskPct := args.RiskPct
	if riskPct == 0 {
		riskPct = 1.0
	}
	status, err := acc.StartBot(ctx, service.BotConfig{
		Instrument:   args.Instrument,
		TickInterval: args.TickInterval,
		RiskPct:      riskPct,
		MaxUnits:     args.MaxUnits,
		Strategy:     service.StrategyConfig{Kind: args.Strategy},
	})
	if err != nil {
		return errContent(fmt.Sprintf("start_bot: %v", err)), nil
	}
	return jsonContent(status), nil
}

func (s *Server) toolStopBot(raw json.RawMessage) (any, *rpcError) {
	var args struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &args); err != nil || args.ID == "" {
		return nil, &rpcError{Code: errInvalidParams, Message: "id is required"}
	}
	if err := s.svc.StopBot(args.ID); err != nil {
		return errContent(fmt.Sprintf("stop_bot: %v", err)), nil
	}
	return textContent(fmt.Sprintf("bot %s stopped", args.ID)), nil
}

func (s *Server) toolGetVersion() (any, *rpcError) {
	return jsonContent(map[string]string{"version": config.Version}), nil
}

func (s *Server) toolGetHealth() (any, *rpcError) {
	return jsonContent(map[string]string{"status": "ok"}), nil
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

// propAccountReadOptional describes the optional account_id argument on read
// tools, which default to the first/default account when it is omitted.
var propAccountReadOptional = prop("string", "OANDA account ID; defaults to the first/default account when omitted")

// propAccountWriteRequired describes the required account_id argument on
// mutating tools, which must always name the account they act on.
var propAccountWriteRequired = prop("string", "OANDA account ID to act on (required for write operations)")

func prop(typ, description string) map[string]any {
	m := map[string]any{"type": typ, "description": description}
	if typ == "array" {
		m["items"] = map[string]any{"type": "string"}
	}
	return m
}
