package broker

import (
	"fmt"
	"strings"

	runtimeorder "github.com/rustyeddy/trader/internal/order"
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
func parseOrderType(s string) (runtimeorder.Type, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "market":
		return runtimeorder.Market, nil
	case "limit":
		return runtimeorder.Limit, nil
	case "stop":
		return runtimeorder.Stop, nil
	case "stop_limit", "stop-limit", "stoplimit":
		return runtimeorder.StopLimit, nil
	default:
		return 0, fmt.Errorf("invalid --type %q: expected market, limit, stop, or stop-limit", s)
	}
}

// parseTimeInForce parses --tif into order.TimeInForce, case-insensitively.
func parseTimeInForce(s string) (runtimeorder.TimeInForce, error) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "GTC":
		return runtimeorder.GTC, nil
	case "DAY":
		return runtimeorder.DAY, nil
	case "IOC":
		return runtimeorder.IOC, nil
	case "FOK":
		return runtimeorder.FOK, nil
	default:
		return 0, fmt.Errorf("invalid --tif %q: expected GTC, DAY, IOC, or FOK", s)
	}
}
