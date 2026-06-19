package trader

import (
	"context"
	"fmt"
)

// Stat is a single labeled measurement returned by an Analyzer.
// Pips is the raw pip count when Value is a pip measurement; zero otherwise.
// Callers can use Pips to convert to a currency amount without re-parsing Value.
type Stat struct {
	Name  string
	Value string
	Pips  float64
}

// Analyzer accumulates statistics over a candle sequence.
type Analyzer interface {
	Name() string
	Update(*CandleTime)
	Stats() []Stat
}

// RunAnalysis walks itr, feeding every candle to each Analyzer.
// It closes itr before returning.
func RunAnalysis(ctx context.Context, itr CandleIterator, analyzers []Analyzer) (err error) {
	if itr == nil {
		return fmt.Errorf("nil candle iterator")
	}
	defer func() {
		if closeErr := itr.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		ct, ok := itr.Next()
		if !ok {
			break
		}
		for _, a := range analyzers {
			a.Update(&ct)
		}
	}
	return itr.Err()
}
