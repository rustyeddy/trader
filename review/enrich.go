package review

import (
	"log/slog"

	"github.com/rustyeddy/trader/market"
)

// EnrichTradeableWithH1 fetches H1 candles and attaches an entry-timing
// refinement via EnrichWithH1, but only when result is already classified
// tradeable in this same call — fetchH1 is never invoked for watch/hot
// pairs, since there is nothing to time the entry of yet. A fetch failure
// is best-effort: it is logged and the pair's classification is returned
// unchanged, never dropped.
func EnrichTradeableWithH1(result ReviewResult, log *slog.Logger, name string, fetchH1 func() ([]market.Candle, error)) ReviewResult {
	if result.Bucket != "tradeable" {
		return result
	}
	h1, err := fetchH1()
	if err != nil {
		log.Warn("review: fetch H1 candles", "instrument", name, "err", err)
		return result
	}
	return EnrichWithH1(result, h1)
}
