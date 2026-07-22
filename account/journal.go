package account

import (
	"context"

	"github.com/rustyeddy/trader/journal"
)

// RunLiveJournal subscribes to the OANDA transaction stream and writes a
// TradeRecord per closed trade to the given Journal. Blocks until ctx is
// cancelled or the stream ends.
//
// If backfillFrom > 0, transactions with ID > backfillFrom are polled and
// replayed into the journal before the stream subscription starts —
// useful for downtime recovery.
//
// botIDLookup, if non-nil, is called on each trade close to tag the journal
// record with the managed bot that opened it. It's injected rather than
// reached via a service-wide registry — that registry (Service.tradeBotMap)
// is a Service-level concern, not per-account state.
func (acct *Account) RunLiveJournal(ctx context.Context, jrnl journal.Journal, backfillFrom int64, botIDLookup func(tradeID string) string) (lastSeenTxID int64, err error) {
	lj := journal.NewLiveJournal(acct.broker(), acct.ID, jrnl, acct.Log)
	if botIDLookup != nil {
		lj.SetBotIDLookup(botIDLookup)
	}

	if backfillFrom > 0 {
		if err := lj.Backfill(ctx, backfillFrom); err != nil {
			return lj.LastSeenTxID(), err
		}
	}

	runErr := lj.Run(ctx)
	if runErr != nil && ctx.Err() == nil {
		return lj.LastSeenTxID(), runErr
	}
	return lj.LastSeenTxID(), nil
}
