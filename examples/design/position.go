package main

type position struct {
	instrument string
	direction  direction
	units      int64
	entryPrice price
	stopLoss   price // protective stop from the opening order; 0 means none
	markPrice  price // latest price the position was valued at: the fill, then each bar's close
}
