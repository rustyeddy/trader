package trader

// Transition shim re-exporting the execution package (accounting + order
// execution, extracted from the root trader package) so existing callers
// compile unchanged during the migration. Remove entries as callers move to
// github.com/rustyeddy/trader/execution directly. See docs/pkg-migration.org.

import "github.com/rustyeddy/trader/execution"

// Accounting / order-execution types.
type (
	Account      = execution.Account
	Broker       = execution.Broker
	Event        = execution.Event
	EventType    = execution.EventType
	Trade        = execution.Trade
	TradeCommon  = execution.TradeCommon
	TradeHistory = execution.TradeHistory
	Position     = execution.Position
	Lot          = execution.Lot
	LotBook      = execution.LotBook
	LotMatch     = execution.LotMatch
	CloseMatcher = execution.CloseMatcher
	FIFOMatcher  = execution.FIFOMatcher
	Request      = execution.Request
	RequestType  = execution.RequestType
	OpenRequest  = execution.OpenRequest
	CloseRequest = execution.CloseRequest
	CloseCause   = execution.CloseCause
)

// Constructors / helpers.
var (
	NewAccount          = execution.NewAccount
	NewBroker           = execution.NewBroker
	NewOpenRequest      = execution.NewOpenRequest
	NewTradeHistory     = execution.NewTradeHistory
	InstrumentPositions = execution.InstrumentPositions
)

// Lot-state values.
const (
	LotNone           = execution.LotNone
	LotOpen           = execution.LotOpen
	LotOpenRequested  = execution.LotOpenRequested
	LotCloseRequested = execution.LotCloseRequested
	LotClosed         = execution.LotClosed
)

// Close-cause values.
const (
	CloseUnknown           = execution.CloseUnknown
	CloseManual            = execution.CloseManual
	CloseStopLoss          = execution.CloseStopLoss
	CloseTakeProfit        = execution.CloseTakeProfit
	CloseBrokerLiquidation = execution.CloseBrokerLiquidation
)

// Request-type values.
const (
	RequestNone       = execution.RequestNone
	RequestMarketOpen = execution.RequestMarketOpen
	RequestLimitOpen  = execution.RequestLimitOpen
	RequestClose      = execution.RequestClose
)

// Event-type values.
const (
	EventOrderFilled    = execution.EventOrderFilled
	EventPositionClosed = execution.EventPositionClosed
)
