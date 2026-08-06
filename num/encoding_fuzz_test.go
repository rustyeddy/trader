package num

import (
	"encoding/json"
	"testing"
)

// FuzzPriceJSONRoundTrip checks that every Price JSON encoding decodes back
// to the identical value, for the full range Parse accepts.
func FuzzPriceJSONRoundTrip(f *testing.F) {
	seeds := []string{"0", "1", "123.45", "0.00000001", "92233720368.54775807"}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		want, err := ParsePrice(s)
		if err != nil {
			return
		}

		data, err := json.Marshal(want)
		if err != nil {
			t.Fatalf("Marshal(%v) failed: %v", want, err)
		}

		var got Price
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("Unmarshal(%q) failed: %v", data, err)
		}
		if !want.Equal(got) {
			t.Fatalf("round trip changed value: %v -> %q -> %v", want, data, got)
		}
	})
}

// FuzzMoneyTextRoundTrip checks the "<amount> <currency>" text form round
// trips for every amount/currency pair ParseMoney accepts.
func FuzzMoneyTextRoundTrip(f *testing.F) {
	f.Add("123.45", "USD")
	f.Add("-15", "EUR")
	f.Add("0", "BTC")
	f.Add("0.00000001", "USDT")

	f.Fuzz(func(t *testing.T, amount, currencyCode string) {
		currency, err := ParseCurrency(currencyCode)
		if err != nil {
			return
		}
		want, err := ParseMoney(amount, currency)
		if err != nil {
			return
		}

		text, err := want.MarshalText()
		if err != nil {
			t.Fatalf("MarshalText(%v) failed: %v", want, err)
		}

		var got Money
		if err := got.UnmarshalText(text); err != nil {
			t.Fatalf("UnmarshalText(%q) failed: %v", text, err)
		}
		if !want.Equal(got) {
			t.Fatalf("round trip changed value: %v -> %q -> %v", want, text, got)
		}
	})
}
