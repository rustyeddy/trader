package main

import "context"

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
