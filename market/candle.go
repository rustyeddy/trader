package market

type Price = int32

type OHLC struct {
	O Price
	H Price
	L Price
	C Price
}
