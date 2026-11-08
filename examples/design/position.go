package main

import "time"

type position struct {
	instrument string
	direction  direction
	units      int64
	entryPrice price
	entryTime  time.Time // the bar the open filled on
	markPrice  price     // latest price the position was valued at: the fill, then each bar's close
}

// trade is a position that actually closed. An open position is not a trade
// yet, and is not a win or a loss.
type trade struct {
	instrument string
	direction  direction
	units      int64
	entryPrice price
	entryTime  time.Time
	exitPrice  price
	exitTime   time.Time
	reason     orderReason // what closed it
	pnl        money       // realized, rounded down once to the cent
}
