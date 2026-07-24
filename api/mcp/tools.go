package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/rustyeddy/trader/config"
	accountsvc "github.com/rustyeddy/trader/service/account"
)

// toolDef describes one MCP tool exposed to the client.
type toolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// tools returns all tools this server exposes.
//
// Kept deliberately minimal: only the account tools (matching `trader
// account list/summary/orders`) and baseline infra checks. Other
// capabilities (backtests, candles, bots, live orders, pip/position calc)
// were removed until a concrete MCP use case needs them — see
// docs/Manual/Architecture.org's "Transport convergence (target)" section.
// Add tools back deliberately, not for parity's sake.
func (s *Server) tools() []toolDef {
	return []toolDef{
		{
			Name:        "list_accounts",
			Description: "List the OANDA accounts the configured token can access, flagging the default (first) account.",
			InputSchema: schema(nil, nil),
		},
		{
			Name:        "account_summary",
			Description: "Mirrors `trader account summary`: return balance/NAV/margin/P&L for one account (account_id given) or every account the configured token can see (account_id omitted), including per-account fetch errors.",
			InputSchema: schema(map[string]any{
				"account_id": propAccountReadOptional,
			}, nil),
		},
		{
			Name:        "account_orders",
			Description: "Mirrors `trader account orders`: return open trades on the resolved account.",
			InputSchema: schema(map[string]any{
				"account_id": propAccountReadOptional,
			}, nil),
		},
		{
			Name:        "get_version",
			Description: "Return the trader server version string.",
			InputSchema: schema(nil, nil),
		},
		{
			Name:        "get_health",
			Description: "Return the server health status.",
			InputSchema: schema(nil, nil),
		},
	}
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

	switch p.Name {
	case "list_accounts":
		return s.toolListAccounts(ctx)
	case "account_summary":
		return s.toolAccountSummary(ctx, p.Arguments)
	case "account_orders":
		return s.toolAccountOrders(ctx, p.Arguments)
	case "get_version":
		return s.toolGetVersion()
	case "get_health":
		return s.toolGetHealth()
	default:
		return nil, &rpcError{Code: errMethodNotFound, Message: fmt.Sprintf("unknown tool: %s", p.Name)}
	}
}

// ── tool implementations ──────────────────────────────────────────────────

func (s *Server) toolListAccounts(ctx context.Context) (any, *rpcError) {
	result, err := accountsvc.List(ctx, accountsvc.AccountCfg{Broker: "oanda"})
	if err != nil {
		return errContent(fmt.Sprintf("list_accounts: %v", err)), nil
	}
	defaultID := accountsvc.DefaultAccountID(result.Accounts, s.accountID)
	out := make([]map[string]any, 0, len(result.Accounts))
	for _, ref := range result.Accounts {
		out = append(out, map[string]any{
			"id":         ref.ID,
			"is_default": ref.ID == defaultID,
		})
	}
	return jsonContent(map[string]any{"accounts": out}), nil
}

// accountCfgFromArgs builds an accountsvc.AccountCfg from the optional
// account_id argument, mirroring cmd/account's --account-id flag.
func accountCfgFromArgs(raw json.RawMessage) accountsvc.AccountCfg {
	id := parseAccountID(raw)
	return accountsvc.AccountCfg{
		Broker:           "oanda",
		AccountID:        id,
		AccountIDChanged: id != "",
	}
}

// toolAccountSummary mirrors `trader account summary`: balance/NAV/margin/
// P&L for one account (account_id given) or every account the configured
// token can see (account_id omitted), including per-account fetch errors.
func (s *Server) toolAccountSummary(ctx context.Context, raw json.RawMessage) (any, *rpcError) {
	results, err := accountsvc.Summary(ctx, accountCfgFromArgs(raw))
	if err != nil {
		return errContent(fmt.Sprintf("account_summary: %v", err)), nil
	}
	out := make([]map[string]any, 0, len(results))
	for _, result := range results {
		entry := map[string]any{"id": result.ID}
		if result.Err != nil {
			entry["error"] = result.Err.Error()
		} else {
			entry["summary"] = result.Summary
		}
		out = append(out, entry)
	}
	return jsonContent(map[string]any{"accounts": out}), nil
}

// toolAccountOrders mirrors `trader account orders`: open trades on the
// resolved account.
func (s *Server) toolAccountOrders(ctx context.Context, raw json.RawMessage) (any, *rpcError) {
	trades, err := accountsvc.Orders(ctx, accountCfgFromArgs(raw))
	if err != nil {
		return errContent(fmt.Sprintf("account_orders: %v", err)), nil
	}
	return jsonContent(trades), nil
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

func prop(typ, description string) map[string]any {
	m := map[string]any{"type": typ, "description": description}
	if typ == "array" {
		m["items"] = map[string]any{"type": "string"}
	}
	return m
}
