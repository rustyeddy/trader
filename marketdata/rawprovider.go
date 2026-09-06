package marketdata

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/rustyeddy/trader/marketdata/internal/provider/alpaca"
	"github.com/rustyeddy/trader/marketdata/internal/provider/oanda"
	"github.com/rustyeddy/trader/marketdata/internal/provider/stooq"
)

// rawRootExists reports whether root already exists on disk. Both
// providers treat a not-yet-created raw root as an empty archive
// rather than an error — nothing has ever been synced/imported there
// yet, which is exactly what an empty raw archive means for a
// read-only operation (Coverage/Plan) that must never create the
// directory itself.
func rawRootExists(root string) bool {
	_, err := os.Stat(root)
	return !errors.Is(err, os.ErrNotExist)
}

// rawPartitionStatus is the provider-neutral outcome of inspecting one
// monthly raw partition file — the common shape coverage.go/plan.go
// actually consume from either oanda.PartitionStatus or
// stooq.PartitionStatus (ADR-047's internal provider seam), so neither
// file needs to import a specific provider package's status type
// directly.
type rawPartitionStatus uint8

const (
	rawPartitionUnknown rawPartitionStatus = iota
	rawPartitionOK
	rawPartitionUnreadable
	rawPartitionMalformed
)

// String returns a human-readable rawPartitionStatus name, used only in
// diagnostic Action.Reason text (plan.go).
func (s rawPartitionStatus) String() string {
	switch s {
	case rawPartitionOK:
		return "ok"
	case rawPartitionUnreadable:
		return "unreadable"
	case rawPartitionMalformed:
		return "malformed"
	default:
		return "unknown"
	}
}

// rawPartitionInfo is the provider-neutral raw-partition summary
// coverage.go's isStale and plan.go's deriveActionsRawBuilt/needsExtend
// actually need, regardless of which concrete provider produced it.
// Every field here has a direct oanda.Partition/stooq.Partition
// counterpart (see the conversion functions below); fields those
// packages track that nothing in this package ever consumes (oanda's
// own DuplicateTimes, IncompleteCount is the one exception kept below
// since coverage.go does report it) are deliberately not carried over.
type rawPartitionInfo struct {
	year            int
	month           time.Month
	status          rawPartitionStatus
	rowCount        int
	incompleteCount int
	lastComplete    bool
	lastTime        time.Time
	fingerprint     string
}

func oandaStatusToRaw(s oanda.PartitionStatus) rawPartitionStatus {
	switch s {
	case oanda.PartitionStatusOK:
		return rawPartitionOK
	case oanda.PartitionStatusUnreadable:
		return rawPartitionUnreadable
	case oanda.PartitionStatusMalformed:
		return rawPartitionMalformed
	default:
		return rawPartitionUnknown
	}
}

func stooqStatusToRaw(s stooq.PartitionStatus) rawPartitionStatus {
	switch s {
	case stooq.PartitionStatusOK:
		return rawPartitionOK
	case stooq.PartitionStatusUnreadable:
		return rawPartitionUnreadable
	case stooq.PartitionStatusMalformed:
		return rawPartitionMalformed
	default:
		return rawPartitionUnknown
	}
}

func alpacaStatusToRaw(s alpaca.PartitionStatus) rawPartitionStatus {
	switch s {
	case alpaca.PartitionStatusOK:
		return rawPartitionOK
	case alpaca.PartitionStatusUnreadable:
		return rawPartitionUnreadable
	case alpaca.PartitionStatusMalformed:
		return rawPartitionMalformed
	default:
		return rawPartitionUnknown
	}
}

// allowsLiveExtend reports whether Plan should consider scheduling an
// ActionDownloadRaw "extend" for m's provider's most recently touched
// month (plan.go's needsExtend). OANDA has a live feed and legitimately
// might have newer data past what the raw archive already holds; Stooq
// is a one-shot offline import (stooq.Import) with nothing further to
// fetch — there is no live feed to check at all — so it always reports
// false (ADR-047's explicit decision on this point, made precisely to
// avoid Plan generating an "extend" action that Sync could never
// execute for a provider with no acquisition client).
func (m *Manager) allowsLiveExtend() bool {
	return m.providerName != "stooq"
}

// rawInventoryLookup returns a lookup map of every raw partition found
// under m.rawRoot for interval, dispatched to the concrete provider
// implementation named by m.providerName (ADR-047's internal provider
// seam — oanda and stooq are the two implementations today, oanda
// unchanged in behavior from before this seam existed). It returns a
// nil map with no error for W1, since raw inspection does not apply to
// a derived interval, and a wrapped ErrInvalidConfig if m.rawRoot is
// empty for any interval that does need it.
//
// A configured m.rawRoot that does not exist on disk at all is treated
// as an empty archive (an empty lookup, no error) rather than
// propagating the provider's own ENOENT failure — see coverage.go's
// original version of this function (before the seam) for the full
// "why" of this rule, which applies identically to both providers.
func (m *Manager) rawInventoryLookup(ctx context.Context, interval Interval) (map[rawPartitionKey]rawPartitionInfo, error) {
	rawInterval, ok := intervalToRawInterval(interval)
	if !ok {
		return nil, nil
	}
	if m.rawRoot == "" {
		return nil, fmt.Errorf("%w: raw root is not configured", ErrInvalidConfig)
	}
	if !rawRootExists(m.rawRoot) {
		return map[rawPartitionKey]rawPartitionInfo{}, nil
	}

	switch m.providerName {
	case "stooq":
		if rawInterval != string(stooq.RawD1) {
			return nil, fmt.Errorf("marketdata: stooq: only %s is supported, got %s", D1, interval)
		}
		inv, err := stooq.Inspect(ctx, m.rawRoot)
		if err != nil {
			return nil, fmt.Errorf("inspect raw archive: %w", err)
		}
		lookup := make(map[rawPartitionKey]rawPartitionInfo, len(inv.Partitions))
		for _, p := range inv.Partitions {
			lookup[rawPartitionKey{p.Symbol, rawInterval, p.Year, p.Month}] = rawPartitionInfo{
				year: p.Year, month: p.Month,
				status: stooqStatusToRaw(p.Status), rowCount: p.RowCount,
				lastComplete: p.LastComplete, lastTime: p.LastTime, fingerprint: p.Fingerprint,
			}
		}
		return lookup, nil
	case "alpaca":
		if rawInterval != string(alpaca.RawD1) {
			return nil, fmt.Errorf("marketdata: alpaca: only %s is supported, got %s", D1, interval)
		}
		inv, err := alpaca.Inspect(ctx, m.rawRoot)
		if err != nil {
			return nil, fmt.Errorf("inspect raw archive: %w", err)
		}
		lookup := make(map[rawPartitionKey]rawPartitionInfo, len(inv.Partitions))
		for _, p := range inv.Partitions {
			lookup[rawPartitionKey{p.Symbol, rawInterval, p.Year, p.Month}] = rawPartitionInfo{
				year: p.Year, month: p.Month,
				status: alpacaStatusToRaw(p.Status), rowCount: p.RowCount,
				lastComplete: p.LastComplete, lastTime: p.LastTime, fingerprint: p.Fingerprint,
			}
		}
		return lookup, nil
	default:
		inv, err := oanda.Inspect(ctx, m.rawRoot)
		if err != nil {
			return nil, fmt.Errorf("inspect raw archive: %w", err)
		}
		lookup := make(map[rawPartitionKey]rawPartitionInfo, len(inv.Partitions))
		for _, p := range inv.Partitions {
			lookup[rawPartitionKey{p.Symbol, string(p.Interval), p.Year, p.Month}] = rawPartitionInfo{
				year: p.Year, month: p.Month,
				status: oandaStatusToRaw(p.Status), rowCount: p.RowCount, incompleteCount: p.IncompleteCount,
				lastComplete: p.LastComplete, lastTime: p.LastTime, fingerprint: p.Fingerprint,
			}
		}
		return lookup, nil
	}
}
