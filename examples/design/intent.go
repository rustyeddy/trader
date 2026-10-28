package main

import "context"

type handleIntent interface {
	handleIntent(context.Context, intent)
}

type intent struct {
	kind string // open, close, noop
	side string // long or short
}
