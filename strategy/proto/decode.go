package proto

import (
	"encoding/json"
	"strings"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/strategy"
)

// Decode parses one frame payload into a message. It is strict, as ADR-094 says:
// the payload must be exactly one JSON object without duplicate member names;
// prices are decimal strings and times UTC timestamps; unknown members are
// ignored in the envelope and in every message except that the run object of a
// Register and each order object of Orders are strict, and a host-owned member of
// an order is refused by name. The error is an *Error: CodeVersion for another
// protocol major, CodeRejected for a bad run, CodeInvalidOrder for a bad order
// object, CodeProtocol for everything else. Decode checks form, not meaning:
// Register.Validate and Orders.Validate check that.
func Decode(frame []byte) (Message, error) {
	if err := checkJSON(frame); err != nil {
		return nil, err
	}
	raw, err := newObj(frame, "message", CodeProtocol)
	if err != nil {
		return nil, err
	}
	h, err := decodeHeader(raw)
	if err != nil {
		return nil, err
	}
	switch h.Type {
	case TypeRegister:
		return decodeRegister(raw, h)
	case TypeAccepted:
		return decodeAccepted(raw, h)
	case TypeBars:
		return decodeBars(raw, h)
	case TypeOrders:
		return decodeOrders(raw, h)
	case TypeDone:
		return decodeDone(raw, h)
	default: // TypeError
		return decodeFailure(raw, h)
	}
}

func decodeHeader(o obj) (Header, error) {
	var h Header
	v, _, err := o.uint("v", true)
	if err != nil {
		return h, err
	}
	if v != Version {
		return h, errorf(CodeVersion, "protocol version %d is not supported (this is %d)", v, Version)
	}
	h.V = Version
	t, err := o.requiredStr("type")
	if err != nil {
		return h, err
	}
	switch h.Type = Type(t); h.Type {
	case TypeRegister, TypeAccepted, TypeBars, TypeOrders, TypeDone, TypeError:
	default:
		return h, errorf(CodeProtocol, "unknown message type %q", t)
	}
	if h.Seq, _, err = o.uint("seq", true); err != nil {
		return h, err
	}
	if h.Seq == 0 {
		return h, errorf(CodeProtocol, "message.seq: starts at 1")
	}
	id, present, err := o.str("run_id", false)
	if err != nil {
		return h, err
	}
	// Register precedes the RunID and an error may precede it; everything else
	// carries it.
	if h.Type != TypeRegister && h.Type != TypeError && (!present || id == "") {
		return h, errorf(CodeProtocol, "message.run_id: required on %s", h.Type)
	}
	// The host issues the RunID in Accepted, so a Register cannot carry one.
	if h.Type == TypeRegister && present {
		return h, errorf(CodeProtocol, "message.run_id: a register carries none; the host issues it in accepted")
	}
	h.RunID = id
	return h, nil
}

func decodeRegister(o obj, h Header) (*Register, error) {
	m := &Register{Header: h}
	var err error
	if m.Name, err = o.requiredStr("name"); err != nil {
		return nil, err
	}
	if m.Version, _, err = o.str("version", false); err != nil {
		return nil, err
	}
	if m.Build, _, err = o.str("build", false); err != nil {
		return nil, err
	}
	if r, ok, err := o.raw("config", false); err != nil {
		return nil, err
	} else if ok {
		if r[0] != '{' {
			return nil, o.fail("config", "must be a JSON object")
		}
		m.Config = append(json.RawMessage(nil), r...)
	}
	if m.Capabilities, _, err = o.strings("capabilities", true); err != nil {
		return nil, err
	}
	run, _, err := o.object("run", true, CodeRejected)
	if err != nil {
		return nil, err
	}
	m.Run, err = decodeRun(run)
	return m, err
}

// decodeRun reads the run object, which is strict: an unknown member is a
// rejection, so a typo cannot silently change what the run asks for.
func decodeRun(o obj) (Run, error) {
	var r Run
	if err := o.only("mode", "interval", "instruments", "from", "to", "warm_bars"); err != nil {
		return r, err
	}
	var err error
	if r.Mode, err = o.requiredStr("mode"); err != nil {
		return r, err
	}
	if r.Interval, err = o.requiredStr("interval"); err != nil {
		return r, err
	}
	if r.Instruments, _, err = o.strings("instruments", true); err != nil {
		return r, err
	}
	if r.From, _, err = o.timestamp("from", true); err != nil {
		return r, err
	}
	if r.To, _, err = o.timestamp("to", false); err != nil {
		return r, err
	}
	n, _, err := o.uint("warm_bars", false)
	r.WarmBars = int(n)
	return r, err
}

func decodeAccepted(o obj, h Header) (*Accepted, error) {
	m := &Accepted{Header: h}
	var err error
	if m.Instruments, _, err = o.strings("instruments", true); err != nil {
		return nil, err
	}
	if m.Interval, err = o.requiredStr("interval"); err != nil {
		return nil, err
	}
	n, _, err := o.uint("warm_bars", false)
	if err != nil {
		return nil, err
	}
	m.WarmBars = int(n)
	ms, _, err := o.uint("reply_timeout_ms", true)
	m.ReplyTimeoutMS = int(ms)
	return m, err
}

func decodeBars(o obj, h Header) (*Bars, error) {
	m := &Bars{Header: h}
	var err error
	if m.FrameAt, _, err = o.timestamp("frame_at", true); err != nil {
		return nil, err
	}
	if m.Warm, _, err = o.boolean("warm", false); err != nil {
		return nil, err
	}
	items, _, err := o.array("bars", true)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, o.fail("bars", "is never empty")
	}
	for i, it := range items {
		b, err := newObj(it, o.where+".bars["+itoa(i)+"]", CodeProtocol)
		if err != nil {
			return nil, err
		}
		bar, err := decodeBar(b)
		if err != nil {
			return nil, err
		}
		m.Bars = append(m.Bars, bar)
	}
	return m, nil
}

func decodeBar(o obj) (marketdata.Bar, error) {
	var b marketdata.Bar
	var err error
	if b.Instrument, err = o.requiredStr("instrument"); err != nil {
		return b, err
	}
	if b.Time, _, err = o.timestamp("time", true); err != nil {
		return b, err
	}
	if b.AvailableAt, _, err = o.timestamp("available_at", true); err != nil {
		return b, err
	}
	vol, _, err := o.uint("volume", false)
	if err != nil {
		return b, err
	}
	b.Volume = int64(vol)
	for _, s := range []struct {
		name string
		dst  *marketdata.OHLC
	}{{"bid", &b.Bid}, {"ask", &b.Ask}, {"mid", &b.Mid}, {"trade", &b.Trade}} {
		sub, ok, err := o.object(s.name, false, CodeProtocol)
		if err != nil {
			return b, err
		}
		if !ok {
			continue
		}
		if *s.dst, err = decodeOHLC(sub); err != nil {
			return b, err
		}
	}
	return b, nil
}

func decodeOHLC(o obj) (marketdata.OHLC, error) {
	var out marketdata.OHLC
	for _, f := range []struct {
		name string
		dst  *num.Price
	}{{"open", &out.Open}, {"high", &out.High}, {"low", &out.Low}, {"close", &out.Close}} {
		p, _, err := o.decimal(f.name, true)
		if err != nil {
			return out, err
		}
		*f.dst = p
	}
	return out, nil
}

func decodeOrders(o obj, h Header) (*Orders, error) {
	m := &Orders{Header: h}
	var err error
	if m.InReplyTo, _, err = o.uint("in_reply_to", true); err != nil {
		return nil, err
	}
	items, _, err := o.array("orders", true)
	if err != nil {
		return nil, err
	}
	for i, it := range items {
		ord, err := newObj(it, o.where+".orders["+itoa(i)+"]", CodeInvalidOrder)
		if err != nil {
			return nil, err
		}
		order, err := decodeOrder(ord)
		if err != nil {
			return nil, err
		}
		m.Orders = append(m.Orders, order)
	}
	return m, nil
}

// orderMembers are the members an order object may carry. Anything else fails the
// run, so a misspelt take_profit is never silently dropped.
var orderMembers = []string{"instrument", "action", "direction", "type", "price",
	"stop_loss", "take_profit", "trail", "expires", "ref"}

// hostOwned are the members only Trader sets. They are compared ignoring case and
// underscores, so accountID, AccountID and account_id are one name.
var hostOwned = map[string]bool{"units": true, "id": true, "accountid": true, "state": true,
	"fillprice": true, "reason": true, "decidedat": true, "history": true}

func decodeOrder(o obj) (strategy.Order, error) {
	var ord strategy.Order
	for _, k := range sortedKeys(o.m) {
		if hostOwned[strings.ToLower(strings.ReplaceAll(k, "_", ""))] {
			return ord, errorf(CodeInvalidOrder, "%s: %q is set by Trader, not by a strategy", o.where, k)
		}
	}
	if err := o.only(orderMembers...); err != nil {
		return ord, err
	}
	var err error
	if ord.Instrument, err = o.requiredStr("instrument"); err != nil {
		return ord, err
	}
	a, err := o.requiredStr("action")
	if err != nil {
		return ord, err
	}
	switch ord.Action = strategy.Action(a); ord.Action {
	case strategy.Open, strategy.Add, strategy.Reduce, strategy.Close, strategy.Amend:
	default:
		return ord, o.fail("action", "%q is not open, add, reduce, close or amend", a)
	}
	if d, ok, err := o.str("direction", false); err != nil {
		return ord, err
	} else if ok {
		switch ord.Direction = strategy.Direction(d); ord.Direction {
		case strategy.Long, strategy.Short:
		default:
			return ord, o.fail("direction", "%q is not long or short", d)
		}
	}
	if t, ok, err := o.str("type", false); err != nil {
		return ord, err
	} else if ok {
		switch ord.Type = strategy.OrderType(t); ord.Type {
		case strategy.Market, strategy.Limit, strategy.Stop:
		default:
			return ord, o.fail("type", "%q is not market, limit or stop", t)
		}
	}
	if ord.Type == "" {
		ord.Type = strategy.Market // the default, so what is decoded is what is encoded
	}
	for _, f := range []struct {
		name string
		dst  *num.Price
	}{{"price", &ord.Price}, {"stop_loss", &ord.StopLoss}, {"take_profit", &ord.TakeProfit}, {"trail", &ord.Trail}} {
		p, ok, err := o.decimal(f.name, false)
		if err != nil {
			return ord, err
		}
		if ok && p.IsZero() {
			return ord, o.fail(f.name, "must be greater than 0 (leave it out to leave it unset)")
		}
		if ok {
			*f.dst = p
		}
	}
	if t, ok, err := o.timestamp("expires", false); err != nil {
		return ord, err
	} else if ok {
		ord.Expires = t
	}
	if ref, ok, err := o.str("ref", false); err != nil {
		return ord, err
	} else if ok {
		if ref == "" {
			return ord, o.fail("ref", "must not be empty (leave it out)")
		}
		ord.Ref = ref
	}
	return ord, nil
}

func decodeDone(o obj, h Header) (*Done, error) {
	m := &Done{Header: h}
	var err error
	if m.Status, err = o.requiredStr("status"); err != nil {
		return nil, err
	}
	switch m.Status {
	case StatusCompleted, StatusCancelled, StatusFailed:
	default:
		return nil, o.fail("status", "%q is not completed, cancelled or failed", m.Status)
	}
	m.Reason, _, err = o.str("reason", false)
	return m, err
}

func decodeFailure(o obj, h Header) (*Failure, error) {
	m := &Failure{Header: h}
	c, err := o.requiredStr("code")
	if err != nil {
		return nil, err
	}
	if m.Code = Code(c); !m.Code.Valid() {
		return nil, o.fail("code", "%q is not an error code of the protocol", c)
	}
	if m.Fatal, _, err = o.boolean("fatal", true); err != nil {
		return nil, err
	}
	m.Message, err = o.requiredStr("message")
	return m, err
}
