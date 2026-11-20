package proto

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/strategy"
)

func itoa(i int) string { return strconv.Itoa(i) }

// The wire shapes. They exist only to fix member order and to keep absent members
// out: the contract is ADR-094, and decoding does not use them.

type wireHead struct {
	V     int    `json:"v"`
	Type  Type   `json:"type"`
	Seq   uint64 `json:"seq"`
	RunID string `json:"run_id,omitempty"`
}

type wireRegister struct {
	wireHead
	Name         string          `json:"name"`
	Version      string          `json:"version"`
	Build        string          `json:"build"`
	Config       json.RawMessage `json:"config"`
	Capabilities []string        `json:"capabilities"`
	Run          wireRun         `json:"run"`
}

type wireRun struct {
	Mode        string   `json:"mode"`
	Interval    string   `json:"interval"`
	Instruments []string `json:"instruments"`
	From        string   `json:"from"`
	To          string   `json:"to,omitempty"`
	WarmBars    int      `json:"warm_bars"`
}

type wireAccepted struct {
	wireHead
	Instruments    []string `json:"instruments"`
	Interval       string   `json:"interval"`
	WarmBars       int      `json:"warm_bars"`
	ReplyTimeoutMS int      `json:"reply_timeout_ms"`
}

type wireBars struct {
	wireHead
	FrameAt string    `json:"frame_at"`
	Warm    bool      `json:"warm"`
	Bars    []wireBar `json:"bars"`
}

type wireOHLC struct {
	Open  string `json:"open"`
	High  string `json:"high"`
	Low   string `json:"low"`
	Close string `json:"close"`
}

type wireBar struct {
	Instrument  string    `json:"instrument"`
	Time        string    `json:"time"`
	AvailableAt string    `json:"available_at"`
	Bid         *wireOHLC `json:"bid,omitempty"`
	Ask         *wireOHLC `json:"ask,omitempty"`
	Mid         *wireOHLC `json:"mid,omitempty"`
	Trade       *wireOHLC `json:"trade,omitempty"`
	Volume      int64     `json:"volume"`
}

type wireOrders struct {
	wireHead
	InReplyTo uint64      `json:"in_reply_to"`
	Orders    []wireOrder `json:"orders"`
}

type wireOrder struct {
	Instrument string `json:"instrument"`
	Action     string `json:"action"`
	Direction  string `json:"direction,omitempty"`
	Type       string `json:"type"`
	Price      string `json:"price,omitempty"`
	StopLoss   string `json:"stop_loss,omitempty"`
	TakeProfit string `json:"take_profit,omitempty"`
	Trail      string `json:"trail,omitempty"`
	Expires    string `json:"expires,omitempty"`
	Ref        string `json:"ref,omitempty"`
}

type wireDone struct {
	wireHead
	Status string `json:"status"`
	Reason string `json:"reason"`
}

type wireFailure struct {
	wireHead
	Code    Code   `json:"code"`
	Fatal   bool   `json:"fatal"`
	Message string `json:"message"`
}

// priceDecimals is the fixed decimals of every price the archive stores.
const priceDecimals = 5

// priceText is exact decimal text: padded to five decimals, or the price's own
// exact text when it has more. It never rounds.
func priceText(p num.Price) string {
	if s, err := p.FormatFixed(priceDecimals); err == nil {
		return s
	}
	return p.String()
}

func ohlcOf(o marketdata.OHLC) *wireOHLC {
	if o.IsZero() {
		return nil
	}
	return &wireOHLC{Open: priceText(o.Open), High: priceText(o.High), Low: priceText(o.Low), Close: priceText(o.Close)}
}

func optPrice(p num.Price) string {
	if p.IsZero() {
		return ""
	}
	return priceText(p)
}

// Encode returns the payload of m (not yet framed), with members in the fixed
// order of ADR-094 and prices and times in their one form, so the same message is
// always the same bytes. V is Version and Type is set from the kind of message
// when left zero; Seq must be at least 1, and RunID is required on every message
// but Register and Failure.
func Encode(m Message) ([]byte, error) {
	h, err := finishHeader(m)
	if err != nil {
		return nil, err
	}
	wh := wireHead(h)
	var v any
	switch m := m.(type) {
	case *Register:
		cfg := m.Config
		if len(cfg) == 0 {
			cfg = json.RawMessage("{}")
		}
		w := wireRegister{wireHead: wh, Name: m.Name, Version: m.Version, Build: m.Build, Config: cfg,
			Capabilities: nonNil(m.Capabilities),
			Run: wireRun{Mode: m.Run.Mode, Interval: m.Run.Interval, Instruments: nonNil(m.Run.Instruments),
				From: formatTime(m.Run.From), WarmBars: m.Run.WarmBars}}
		if !m.Run.To.IsZero() {
			w.Run.To = formatTime(m.Run.To)
		}
		v = w
	case *Accepted:
		v = wireAccepted{wireHead: wh, Instruments: nonNil(m.Instruments), Interval: m.Interval,
			WarmBars: m.WarmBars, ReplyTimeoutMS: m.ReplyTimeoutMS}
	case *Bars:
		w := wireBars{wireHead: wh, FrameAt: formatTime(m.FrameAt), Warm: m.Warm, Bars: []wireBar{}}
		for _, b := range m.Bars {
			w.Bars = append(w.Bars, wireBar{Instrument: b.Instrument, Time: formatTime(b.Time),
				AvailableAt: formatTime(b.AvailableAt), Bid: ohlcOf(b.Bid), Ask: ohlcOf(b.Ask),
				Mid: ohlcOf(b.Mid), Trade: ohlcOf(b.Trade), Volume: b.Volume})
		}
		v = w
	case *Orders:
		w := wireOrders{wireHead: wh, InReplyTo: m.InReplyTo, Orders: []wireOrder{}}
		for _, o := range m.Orders {
			w.Orders = append(w.Orders, wireOrderOf(o))
		}
		v = w
	case *Done:
		v = wireDone{wireHead: wh, Status: m.Status, Reason: m.Reason}
	case *Failure:
		v = wireFailure{wireHead: wh, Code: m.Code, Fatal: m.Fatal, Message: m.Message}
	default:
		return nil, fmt.Errorf("proto: cannot encode %T", m)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

func wireOrderOf(o strategy.Order) wireOrder {
	t := string(o.Type)
	if t == "" {
		t = string(strategy.Market)
	}
	w := wireOrder{Instrument: o.Instrument, Action: string(o.Action), Direction: string(o.Direction), Type: t,
		Price: optPrice(o.Price), StopLoss: optPrice(o.StopLoss), TakeProfit: optPrice(o.TakeProfit),
		Trail: optPrice(o.Trail), Ref: o.Ref}
	if !o.Expires.IsZero() {
		w.Expires = formatTime(o.Expires)
	}
	return w
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// finishHeader fills V and Type from the message and checks the rest.
func finishHeader(m Message) (Header, error) {
	h := m.Head()
	if h.V == 0 {
		h.V = Version
	}
	if h.V != Version {
		return *h, fmt.Errorf("proto: cannot encode protocol version %d", h.V)
	}
	var want Type
	switch m.(type) {
	case *Register:
		want = TypeRegister
	case *Accepted:
		want = TypeAccepted
	case *Bars:
		want = TypeBars
	case *Orders:
		want = TypeOrders
	case *Done:
		want = TypeDone
	case *Failure:
		want = TypeError
	}
	if h.Type == "" {
		h.Type = want
	}
	if h.Type != want {
		return *h, fmt.Errorf("proto: a %T is not a %q message", m, h.Type)
	}
	if h.Seq == 0 {
		return *h, fmt.Errorf("proto: seq starts at 1")
	}
	if h.RunID != "" && want == TypeRegister {
		return *h, fmt.Errorf("proto: a register carries no run_id")
	}
	if h.RunID == "" && want != TypeRegister && want != TypeError {
		return *h, fmt.Errorf("proto: a %s message needs a run_id", want)
	}
	return *h, nil
}

// Write encodes m and writes it as one frame.
func Write(w io.Writer, m Message, max int) error {
	payload, err := Encode(m)
	if err != nil {
		return err
	}
	return WriteFrame(w, payload, max)
}

// Read reads one frame and decodes it.
func Read(r io.Reader, max int) (Message, error) {
	payload, err := ReadFrame(r, max)
	if err != nil {
		return nil, err
	}
	return Decode(payload)
}
