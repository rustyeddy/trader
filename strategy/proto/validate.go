package proto

import (
	"slices"
	"strings"
	"time"
	"unicode"
)

// Validate checks what a Register asks for, as far as the message alone says: a
// name, the closed set of capabilities (bars is required), a known mode (live is
// reserved), a known interval, distinct instruments, a range, and warm-up only
// with the warm capability. Whether the instruments exist, whether the host
// supports the mode, and policy are the host's to decide. Every failure is a
// CodeRejected *Error.
func (m *Register) Validate() error {
	if m.Name == "" {
		return errorf(CodeRejected, "register.name is required")
	}
	if !slices.Contains(m.Capabilities, CapBars) {
		return errorf(CodeRejected, "register.capabilities must include %q", CapBars)
	}
	seen := map[string]bool{}
	for _, c := range m.Capabilities {
		if c != CapBars && c != CapWarm {
			return errorf(CodeRejected, "register.capabilities: %q is not a capability of this protocol", c)
		}
		if seen[c] {
			return errorf(CodeRejected, "register.capabilities: %q is repeated", c)
		}
		seen[c] = true
	}
	r := m.Run
	switch r.Mode {
	case ModeBacktest, ModePaper:
	case ModeLive:
		return errorf(CodeRejected, "register.run.mode: %q is reserved and not available", r.Mode)
	default:
		return errorf(CodeRejected, "register.run.mode: %q is not backtest or paper", r.Mode)
	}
	if r.Interval != "H1" && r.Interval != "D1" {
		return errorf(CodeRejected, "register.run.interval: %q is not H1 or D1", r.Interval)
	}
	if len(r.Instruments) == 0 {
		return errorf(CodeRejected, "register.run.instruments is empty")
	}
	seen = map[string]bool{}
	for _, in := range r.Instruments {
		if in == "" || strings.IndexFunc(in, func(c rune) bool { return unicode.IsSpace(c) || unicode.IsControl(c) }) >= 0 {
			return errorf(CodeRejected, "register.run.instruments: %q is not an instrument symbol", in)
		}
		if seen[in] {
			return errorf(CodeRejected, "register.run.instruments: %q is repeated", in)
		}
		seen[in] = true
	}
	if r.From.IsZero() {
		return errorf(CodeRejected, "register.run.from is required")
	}
	if r.Mode == ModeBacktest && r.To.IsZero() {
		return errorf(CodeRejected, "register.run.to is required for a backtest")
	}
	if !r.To.IsZero() && !r.To.After(r.From) {
		return errorf(CodeRejected, "register.run.to must be after from")
	}
	if r.WarmBars > 0 && !slices.Contains(m.Capabilities, CapWarm) {
		return errorf(CodeRejected, "register.run.warm_bars needs the %q capability", CapWarm)
	}
	return nil
}

// Validate checks every order against the frame it answers (strategy.Order.Validate),
// naming the order. A failure is a CodeInvalidOrder *Error; nothing from the
// message is to be submitted when it fails (ADR-094 section 6).
func (m *Orders) Validate(frameAt time.Time) error {
	for i, o := range m.Orders {
		if err := o.Validate(frameAt); err != nil {
			return errorf(CodeInvalidOrder, "orders[%d]: %v", i, err)
		}
	}
	return nil
}
