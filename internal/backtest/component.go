package backtest

import (
	"encoding/json"
	"errors"
	"fmt"
)

// ErrInvalidComponentInfo marks a NewComponentInfo call missing a
// required field or given parameters that fail to marshal as JSON.
var ErrInvalidComponentInfo = errors.New("backtest: invalid component info")

// ComponentInfo identifies one configured, composition-root-
// constructed component — a risk.Rule, or a fill/slippage/commission
// model — with enough detail to reproduce the run it was part of
// (issue #215, M5-07 review). Neither risk.Rule nor the simulator's
// own model interfaces expose their own configured parameters (a
// risk.Rule exposes only Name(), for example), and this package does
// not add an introspection capability to either: the composition root
// already knows the exact values it constructed a component with, so
// it records them here directly rather than the component describing
// itself. This is the same "run composition, not the execution object,
// records what it built" principle #215's own design review settled
// on for strategy parameters.
//
// ComponentInfo is immutable: construct it with NewComponentInfo,
// never a struct literal — Parameters returns a defensive copy of its
// own canonically marshaled bytes, and NewComponentInfo canonicalizes
// (re-marshals) whatever parameters value it is given so two calls
// describing the same logical configuration always produce identical
// bytes regardless of whitespace or field-ordering in however the
// caller happened to construct that value.
type ComponentInfo struct {
	name       string
	version    string
	parameters json.RawMessage
}

// NewComponentInfo returns a ComponentInfo. name is required.
// parameters is canonically marshaled via json.Marshal; pass nil for a
// component with no configurable parameters. version is optional
// (empty when the component has no independent version identity of
// its own, distinct from name).
func NewComponentInfo(name, version string, parameters any) (ComponentInfo, error) {
	if name == "" {
		return ComponentInfo{}, fmt.Errorf("%w: name must be set", ErrInvalidComponentInfo)
	}
	data, err := json.Marshal(parameters)
	if err != nil {
		return ComponentInfo{}, fmt.Errorf("%w: marshaling parameters: %v", ErrInvalidComponentInfo, err)
	}
	return ComponentInfo{name: name, version: version, parameters: data}, nil
}

// Name returns the component's configured name (for example
// "max_position_quantity", or a fill model's own ModelInfo.Name).
func (c ComponentInfo) Name() string { return c.name }

// Version returns the component's configured version, or "" if it has
// none of its own.
func (c ComponentInfo) Version() string { return c.version }

// Parameters returns a defensive copy of the component's own
// canonically marshaled JSON parameters. It is "null" (json.Marshal's
// own encoding of a nil value) for a component with no configurable
// parameters.
func (c ComponentInfo) Parameters() json.RawMessage {
	return append(json.RawMessage(nil), c.parameters...)
}

// Equal reports whether c and o describe the same component: equal
// Name, Version, and byte-identical canonical Parameters.
func (c ComponentInfo) Equal(o ComponentInfo) bool {
	return c.name == o.name && c.version == o.version && string(c.parameters) == string(o.parameters)
}
