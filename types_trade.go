package trader

type TradeCommon struct {
	ID         string
	Instrument string
	Side       // Long or Sort
	Units
	Stop Price
	Take Price
}

type Trade struct {
	*TradeCommon
	OpenPrice Price
	OpenTime  Timestamp
	FillPrice Price
	FillTime  Timestamp
	PNL       Money // account currency (best-effort)
}

