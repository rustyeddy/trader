package main

import (
	"context"
	"errors"
	"fmt"
)

type intentHandler interface {
	handleIntent(context.Context, intent) error
}

type direction uint8

const (
	noDirection direction = iota
	long
	short
)

type action uint8

const (
	hold action = iota
	open
	add
	close
	reduce
)

type intent struct {
	instrument string
	direction  direction
	action     action
}

// errInvalidIntent marks an intent whose fields are not a legal combination.
// It is a strategy bug, not a trading outcome.
var errInvalidIntent = errors.New("invalid intent")

// validate checks that the intent is a legal combination of fields. The zero
// value is hold and is a valid no-op; a hold may also name its instrument, as
// strategies should so each intent identifies what it refers to. It is pure: stateful checks (such as a
// reduce larger than the position) belong to the handler.
//
// Add and reduce are only checked for instrument and direction; their size
// rules wait on the size type being defined.
func (i intent) validate() error {
	if i.direction > short {
		return fmt.Errorf("%w: unknown direction %d", errInvalidIntent, i.direction)
	}
	switch i.action {
	case hold:
		return nil
	case open:
		if i.instrument == "" {
			return fmt.Errorf("%w: open requires an instrument", errInvalidIntent)
		}
		if i.direction == noDirection {
			return fmt.Errorf("%w: open requires a direction", errInvalidIntent)
		}
	case add, reduce, close:
		if i.instrument == "" {
			return fmt.Errorf("%w: %s requires an instrument", errInvalidIntent, i.action)
		}
		if i.direction != noDirection {
			return fmt.Errorf("%w: %s takes no direction (got %s)", errInvalidIntent, i.action, i.direction)
		}
	default:
		return fmt.Errorf("%w: unknown action %d", errInvalidIntent, i.action)
	}
	return nil
}

func (d direction) String() string {
	switch d {
	case long:
		return "long"
	case short:
		return "short"
	}
	return "none"
}

func (a action) String() string {
	switch a {
	case hold:
		return "hold"
	case open:
		return "open"
	case add:
		return "add"
	case close:
		return "close"
	case reduce:
		return "reduce"
	}
	return "unknown"
}
