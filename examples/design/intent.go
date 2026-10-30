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
