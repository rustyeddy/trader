package data

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	svc "github.com/rustyeddy/trader/internal/service/marketdata"
)

func TestStooqArchiveErrorAddsFlagGuidance(t *testing.T) {
	wrap := func(err error) error { return fmt.Errorf("%w: detail", err) }
	tests := map[string]struct {
		err  error
		want string
	}{
		"root not configured": {wrap(svc.ErrArchiveRootNotConfigured), "stooq archive root is not configured; provide --archive or --archive-root"},
		"not found":           {wrap(svc.ErrArchiveNotFound), `no Stooq archive for SPY found under "/arch"; provide --archive`},
		"ambiguous":           {wrap(svc.ErrAmbiguousArchive), `multiple Stooq archives for SPY found under "/arch"; provide --archive`},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			assert.EqualError(t, stooqArchiveError(tc.err, "SPY", "/arch"), tc.want)
		})
	}
	other := errors.New("boom")
	assert.Same(t, other, stooqArchiveError(other, "SPY", "/arch"))
}

func TestFullReimportNote(t *testing.T) {
	assert.Empty(t, fullReimportNote(false))
	assert.Contains(t, fullReimportNote(true), "full re-import")
}
