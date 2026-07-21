package account

import "github.com/rustyeddy/trader/types"

// Order is a broker-agnostic request to open or close a position, submitted
// through a Broker (see brokers.Broker). Distinct from OpenRequest/CloseRequest
// in trade_request.go, which are the backtest engine's internal
// request-to-ledger types — Order is the external representation a
// brokers/<name> package translates to/from its own wire format.
type Order struct {
	ID         string
	Instrument string
	Side       types.Side
	Units      int64
	StopPrice  types.Price // 0 = no stop
	TakePrice  types.Price // 0 = no take-profit
}
