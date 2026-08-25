package main

import (
	"fmt"
	"strings"

	"github.com/rustyeddy/trader/order"
)

// parseOrderSide parses --side into order.Side, case-insensitively.
func parseOrderSide(s string) (order.Side, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "buy":
		return order.Buy, nil
	case "sell":
		return order.Sell, nil
	default:
		return 0, fmt.Errorf("invalid --side %q: expected buy or sell", s)
	}
}

// parseOrderType parses --type into order.Type, case-insensitively.
func parseOrderType(s string) (order.Type, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "market":
		return order.Market, nil
	case "limit":
		return order.Limit, nil
	case "stop":
		return order.Stop, nil
	case "stop_limit", "stop-limit", "stoplimit":
		return order.StopLimit, nil
	default:
		return 0, fmt.Errorf("invalid --type %q: expected market, limit, stop, or stop-limit", s)
	}
}

// parseTimeInForce parses --tif into order.TimeInForce, case-insensitively.
func parseTimeInForce(s string) (order.TimeInForce, error) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "GTC":
		return order.GTC, nil
	case "DAY":
		return order.DAY, nil
	case "IOC":
		return order.IOC, nil
	case "FOK":
		return order.FOK, nil
	default:
		return 0, fmt.Errorf("invalid --tif %q: expected GTC, DAY, IOC, or FOK", s)
	}
}
