package trader

import "github.com/rustyeddy/trader/types"

type BA = types.BA
type Tick = types.Tick
type Candle = types.Candle
type CandleTime = types.CandleTime
type CandleSet = types.CandleSet
type Instrument = types.Instrument

var Instruments = types.Instruments
var InstrumentList = types.InstrumentList
var Symmap = types.Symmap

func GetInstrument(symbol string) *Instrument {
	return types.GetInstrument(symbol)
}

func NormalizeInstrument(sym string) string {
	return types.NormalizeInstrument(sym)
}

func NewMonthlyCandleSet(inst string, tf types.Timeframe, monthStart types.Timestamp, scale types.Scale6, source string) (*CandleSet, error) {
	return types.NewMonthlyCandleSet(inst, tf, monthStart, scale, source)
}
