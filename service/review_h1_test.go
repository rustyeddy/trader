package service

import (
	"errors"
	"testing"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/review"
	"github.com/stretchr/testify/assert"
)

// TestEnrichTradeableWithH1_NotCalledForNonTradeableBucket is the key
// behavioral proof for issue #166: H1 is only ever fetched for pairs
// already classified "tradeable" — Watch and Hot pairs must never trigger
// an H1 fetch at all.
func TestEnrichTradeableWithH1_NotCalledForNonTradeableBucket(t *testing.T) {
	for _, bucket := range []string{"watch", "hot"} {
		called := false
		fetchH1 := func() ([]market.Candle, error) {
			called = true
			return nil, nil
		}

		result := review.ReviewResult{Instrument: "EURUSD", Bucket: bucket}
		got := enrichTradeableWithH1(result, discardLogger(), "EURUSD", fetchH1)

		assert.False(t, called, "fetchH1 must not be called for bucket %q", bucket)
		assert.Equal(t, result, got, "result for bucket %q must be returned unchanged", bucket)
	}
}

func TestEnrichTradeableWithH1_CalledForTradeableBucket(t *testing.T) {
	called := false
	fetchH1 := func() ([]market.Candle, error) {
		called = true
		return nil, nil
	}

	result := review.ReviewResult{Instrument: "EURUSD", Bucket: "tradeable"}
	enrichTradeableWithH1(result, discardLogger(), "EURUSD", fetchH1)

	assert.True(t, called, "fetchH1 must be called for a tradeable pair")
}

func TestEnrichTradeableWithH1_FetchErrorIsBestEffort(t *testing.T) {
	fetchH1 := func() ([]market.Candle, error) {
		return nil, errors.New("boom")
	}

	result := review.ReviewResult{Instrument: "EURUSD", Bucket: "tradeable"}
	got := enrichTradeableWithH1(result, discardLogger(), "EURUSD", fetchH1)

	assert.Equal(t, "tradeable", got.Bucket, "a fetch failure must not change or drop the classification")
	assert.Equal(t, result, got, "a fetch failure leaves the result exactly as classified")
}
