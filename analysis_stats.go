package trader

import "fmt"

func missingInstrumentStats() []Stat {
	return []Stat{{Name: "error", Value: "missing instrument"}}
}

func pipStat(name string, pips float64, precision int) Stat {
	return Stat{
		Name:  name,
		Value: fmt.Sprintf("%.*f pips", precision, pips),
		Pips:  pips,
	}
}
