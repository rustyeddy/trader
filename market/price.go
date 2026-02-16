package market

type Price = int32

// This should become part of the instrumnet
const Scale float64 = 100_000.0 // EURUSD pipette scale (1.23456 -> 123456)

func ToFloat(p Price) float64 {
	return float64(p) / Scale
}

func FromFloat(x float64) Price {
	return Price(x*Scale + 0.5)
}
