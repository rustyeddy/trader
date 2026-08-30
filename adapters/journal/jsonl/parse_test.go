package jsonl_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/adapters/journal/jsonl"
)

// TestReaderRejectsEveryUnrecognizedEnumString proves each wire enum
// field's parser (order.Side/Type/TimeInForce/Status/RejectReason/
// PositionSide/IntentKind, broker.AccountStatus) rejects an
// unrecognized string as ErrCorruptEntry rather than silently
// defaulting or panicking, for every wire shape a hand-edited or
// corrupted journal line could plausibly smuggle a bad value into.
func TestReaderRejectsEveryUnrecognizedEnumString(t *testing.T) {
	validListing := `{"instrument_id":"fx:EUR/USD","provider":"sim","symbol":"EUR_USD","spec":{"tick_size":"0.00001","quantity_increment":"1","multiplier":"1","settlement_currency":"USD"},"tradable":true}`

	cases := map[string]string{
		"intent kind":     `{"run_id":"x","sequence":1,"metadata":{"timestamp":"2024-01-01T00:00:00Z"},"kind":"intent","intent":{"intent_id":"x","kind":"bogus","instrument":"fx:EUR/USD","metadata":{}}}`,
		"side":            `{"run_id":"x","sequence":1,"metadata":{"timestamp":"2024-01-01T00:00:00Z"},"kind":"intent","intent":{"intent_id":"x","kind":"enter","instrument":"fx:EUR/USD","side":"bogus","metadata":{}}}`,
		"order type":      `{"run_id":"x","sequence":1,"metadata":{"timestamp":"2024-01-01T00:00:00Z"},"kind":"proposal","proposal":{"listing":` + validListing + `,"account_id":"x","side":"buy","type":"bogus","time_in_force":"gtc","quantity":"1000","metadata":{}}}`,
		"time in force":   `{"run_id":"x","sequence":1,"metadata":{"timestamp":"2024-01-01T00:00:00Z"},"kind":"proposal","proposal":{"listing":` + validListing + `,"account_id":"x","side":"buy","type":"market","time_in_force":"bogus","quantity":"1000","metadata":{}}}`,
		"order status":    `{"run_id":"x","sequence":1,"metadata":{"timestamp":"2024-01-01T00:00:00Z"},"kind":"order","order":{"request":{"proposal":{"listing":` + validListing + `,"account_id":"x","side":"buy","type":"market","time_in_force":"gtc","quantity":"1000","metadata":{}},"order_id":"x"},"status":"bogus","filled_quantity":"0"}}`,
		"position side":   `{"run_id":"x","sequence":1,"metadata":{"timestamp":"2024-01-01T00:00:00Z"},"kind":"trade","trade":{"account_id":"x","listing":` + validListing + `,"side":"bogus","entry_fill_ids":["x"],"opened_at":"2024-01-01T00:00:00Z","realized_pnl":{"amount":"0","currency":"USD"},"costs":{"amount":"0","currency":"USD"}}}`,
		"account status":  `{"run_id":"x","sequence":1,"metadata":{"timestamp":"2024-01-01T00:00:00Z"},"kind":"status","status":{"state":"bogus"}}`,
		"reject reason":   `{"run_id":"x","sequence":1,"metadata":{"timestamp":"2024-01-01T00:00:00Z"},"kind":"order","order":{"request":{"proposal":{"listing":` + validListing + `,"account_id":"x","side":"buy","type":"market","time_in_force":"gtc","quantity":"1000","metadata":{}},"order_id":"x"},"status":"rejected","filled_quantity":"0","rejection":{"reason":"bogus"}}}`,
		"instrument kind": `{"run_id":"x","sequence":1,"metadata":{"timestamp":"2024-01-01T00:00:00Z"},"kind":"trade","trade":{"account_id":"x","listing":{"instrument_id":"eq:NASDAQ:AAPL","provider":"sim","symbol":"AAPL","spec":{"tick_size":"0.01","quantity_increment":"1","multiplier":"1","settlement_currency":"USD"},"tradable":true},"side":"long","entry_fill_ids":["x"],"opened_at":"2024-01-01T00:00:00Z","realized_pnl":{"amount":"0","currency":"USD"},"costs":{"amount":"0","currency":"USD"}}}`,
	}

	for name, line := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "run.jsonl")
			appendRaw(t, path, line)

			r, err := jsonl.OpenReader(path)
			require.NoError(t, err)
			defer func() { _ = r.Close() }()

			_, err = r.Next(context.Background())
			require.ErrorIs(t, err, jsonl.ErrCorruptEntry)
		})
	}
}
