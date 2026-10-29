package main

import "context"

type intentHandler interface {
	handleIntent(context.Context, intent) error
}

type intent struct {
	kind string // open, close, noop
	side string // long or short
}
