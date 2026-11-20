// Package proto is the wire contract of external strategies (ADR-094): the
// messages, their framing and a strict codec for them. It knows nothing about
// networks or listeners. The bytes are defined by the ADR, not by Go struct tags:
// every message is encoded explicitly and decoded by a strict reader that
// rejects what the ADR rejects (duplicate keys, numbers where decimals belong,
// unknown or host-owned order members, and so on).
package proto

import (
	"encoding/json"
	"time"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/strategy"
)

// Version is the protocol major this package speaks.
const Version = 1

// Type names a message.
type Type string

// The message types.
const (
	TypeRegister Type = "register"
	TypeAccepted Type = "accepted"
	TypeBars     Type = "bars"
	TypeOrders   Type = "orders"
	TypeDone     Type = "done"
	TypeError    Type = "error"
)

// Header is the envelope every message carries. RunID is empty on a Register
// (the host issues it in Accepted) and on an Error sent before Accepted.
type Header struct {
	V     int
	Type  Type
	Seq   uint64
	RunID string
}

// Message is one of Register, Accepted, Bars, Orders, Done and Failure.
type Message interface {
	// Head returns the message's header.
	Head() *Header
}

// Modes of a run.
const (
	ModeBacktest = "backtest"
	ModePaper    = "paper"
	ModeLive     = "live" // reserved: refused
)

// Capabilities a worker may declare.
const (
	CapBars = "bars"
	CapWarm = "warm"
)

// Run is what a worker asks to run.
type Run struct {
	Mode        string
	Interval    string
	Instruments []string // in the order the worker wants them delivered
	From        time.Time
	To          time.Time // zero for a run with no end (paper)
	WarmBars    int
}

// Register is the first message of a connection, from the worker.
type Register struct {
	Header
	Name         string
	Version      string
	Build        string
	Config       json.RawMessage // opaque JSON object, or nil
	Capabilities []string
	Run          Run
}

// Accepted is Trader's reply to an acceptable Register: it carries the RunID.
type Accepted struct {
	Header
	Instruments    []string
	Interval       string
	WarmBars       int
	ReplyTimeoutMS int
}

// Bars is one availability instant, from Trader.
type Bars struct {
	Header
	FrameAt time.Time
	Warm    bool
	Bars    []marketdata.Bar
}

// Orders is the worker's reply to one Bars: possibly none.
type Orders struct {
	Header
	InReplyTo uint64
	Orders    []strategy.Order
}

// Done ends a run, from Trader.
type Done struct {
	Header
	Status string // completed, cancelled or failed
	Reason string
}

// Done statuses.
const (
	StatusCompleted = "completed"
	StatusCancelled = "cancelled"
	StatusFailed    = "failed"
)

// Failure is the "error" message: either side sends it and closes. Every v1
// error is fatal.
type Failure struct {
	Header
	Code    Code
	Message string
	Fatal   bool
}

// Head implements Message.
func (h *Header) Head() *Header { return h }
