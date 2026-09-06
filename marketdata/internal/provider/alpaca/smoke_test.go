//go:build alpacasmoke

package alpaca

// This file is the opt-in, real-credential integration/smoke path
// issue #297 (EQ-04) asks for: "a small opt-in integration/smoke path
// can verify the real Alpaca API when credentials are available."
//
// # Why this is not env-var-gated, and not fullarchive-gated either
//
// An earlier revision of this file gated on two environment variables
// instead (ALPACA_API_KEY_ID/ALPACA_API_SECRET_KEY), reasoning that an
// operator should never be encouraged to paste a live credential into
// a tracked source file. That reasoning is still correct on its own,
// but it collides with a real, mechanically-enforced constraint this
// repository already has: config/arch_test.go's
// TestDomainPackagesDoNotReadEnvOrFlags fails the build for *any*
// os.Getenv call — test file or not — outside config/, cmd/, or
// test/, and marketdata/internal/provider/alpaca cannot be relocated
// to any of those (Go's internal/ import-visibility rule confines it
// to the marketdata/ subtree, the same constraint
// import_fullarchive_test.go's own doc comment already notes for
// stooq). Adding an exemption to that check would be loosening a
// mechanically-enforced architectural invariant, which is exactly the
// kind of decision CONTRIBUTING.org asks an agent to escalate rather
// than make unilaterally — so this file conforms to the existing rule
// instead of asking for an exception.
//
// The resulting design is stooq/import_fullarchive_test.go's own
// pattern (a local, uncommitted, build-tag-gated constant an operator
// edits by hand and never commits), applied to a credential pair
// instead of a filesystem path. It is gated behind its own
// "alpacasmoke" build tag — not the existing "fullarchive" tag — so
// that a checkout already configured for a real fullarchive run does
// not also silently attempt a live network call against Alpaca; the
// two opt-in paths are independent and an operator enables each
// deliberately. The build tag additionally keeps this file, and its
// os.Getenv-free but still credential-shaped constants, out of every
// normal `go test ./...` compilation entirely — matching
// import_fullarchive_test.go's own "excluded from normal `go test
// ./...` / `make check`" contract exactly, rather than relying solely
// on a runtime skip.
//
// To run this test against the real Alpaca Market Data API, edit the
// two constants below locally (never commit the edit) and run:
//
//	go test -tags alpacasmoke ./marketdata/internal/provider/alpaca/... \
//	    -run TestSmokeFetchBars -v
//
// A successful run is the first real-world confirmation of this
// package's assumed wire shape (see doc.go and wireshape.go); a
// failure here — especially a JSON-decoding error — is the expected
// signal that wireshape.go's assumptions need correcting against the
// real API, not that client.go's request/retry/pagination logic is
// wrong.

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// alpacaSmokeKeyID and alpacaSmokeSecretKey name the real Alpaca
// credential pair TestSmokeFetchBars uses. They intentionally have no
// real default and ship empty, for the identical reason
// stooq/import_fullarchive_test.go's fullArchiveCSVPath does: an
// operator wanting to run this test edits these constants locally,
// then runs the command above. The edit is never committed; the test
// skips itself whenever either constant is empty, which is always
// true for a fresh checkout.
const alpacaSmokeKeyID = ""
const alpacaSmokeSecretKey = ""
const alpacaSmokeBaseURL = "https://data.alpaca.markets"

// TestSmokeFetchBars fetches a short, recent SPY daily-bar range from
// the real Alpaca Market Data API and checks the shape of what comes
// back. It skips (not fails) when alpacaSmokeKeyID/alpacaSmokeSecretKey
// are empty.
func TestSmokeFetchBars(t *testing.T) {
	if alpacaSmokeKeyID == "" || alpacaSmokeSecretKey == "" {
		t.Skip("alpacaSmokeKeyID/alpacaSmokeSecretKey are empty; edit the constants in this file to point at a real Alpaca credential pair to run this test")
	}

	c, err := NewClient(ClientConfig{
		BaseURL:    alpacaSmokeBaseURL,
		Credential: StaticCredential{KeyID: alpacaSmokeKeyID, SecretKey: alpacaSmokeSecretKey},
	})
	require.NoError(t, err)

	// A short, recent, historical range: recent enough to almost
	// certainly be available on Alpaca's free IEX feed, but old enough
	// (ending several days before "now") to avoid any ambiguity about
	// whether today's still-forming session bar would be included.
	to := time.Now().UTC().AddDate(0, 0, -5)
	from := to.AddDate(0, 0, -10)

	records, err := c.FetchBars(context.Background(), BarRequest{Symbol: "SPY", From: from, To: to})
	require.NoError(t, err, "a decoding error here means wireshape.go's assumed response shape needs correcting against the real API")
	assert.NotEmpty(t, records, "expected at least one SPY trading day in a 10-day recent historical window")

	for _, r := range records {
		assert.True(t, r.Time.Equal(r.Time.Truncate(24*time.Hour)), "record time must be midnight UTC of its trading date, got %s", r.Time)
		assert.False(t, r.Open.IsZero() && r.Close.IsZero(), "expected non-zero OHLC for %s", r.Time)
	}
	t.Logf("fetched %d real SPY daily bars from %s to %s", len(records), from.Format("2006-01-02"), to.Format("2006-01-02"))
}
