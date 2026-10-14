package oanda

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/rustyeddy/trader/internal/accountactivity"
	"github.com/rustyeddy/trader/internal/clock"
	"github.com/rustyeddy/trader/num"
)

// Provider is the provider name in every stream and Key this source emits.
const Provider = "oanda"

// DefaultPageSize is how many transaction IDs one page covers. OANDA's
// idrange endpoint accepts up to 1000.
const DefaultPageSize = 500

// Bounds optionally limits a run to a time window, for bootstrapping
// existing history. The window is half-open, [From, To), like every Trader
// range. OANDA itself treats both ends as inclusive, so the source asks for
// the instant just before To; a transaction exactly at To is not imported.
// From is required when Bounds is used. A zero To means up to now, taken
// from Config.Clock.
type Bounds struct {
	From, To time.Time
}

func (b Bounds) set() bool { return !b.From.IsZero() || !b.To.IsZero() }

// maxListSpan is the longest time span OANDA accepts in one transactions
// list request.
const maxListSpan = 365 * 24 * time.Hour

// Config configures a Source.
type Config struct {
	Client      *Client
	Environment accountactivity.Environment
	AccountID   string
	// Bounds, if set, limits the run to a time window.
	Bounds Bounds
	// Clock supplies "now" for a bound with no end. Required when Bounds has
	// a From but no To.
	Clock clock.Clock
	// PageSize is the ID span of one page; zero means DefaultPageSize.
	PageSize int64
}

// Source is an accountactivity.Source over one OANDA account. Construct one
// per sync run: it resolves the account's currency and end of history on
// first use and keeps them for the rest of the run, so a run reads a stable
// range even while the account keeps trading. A later run, with a new Source,
// picks up what arrived since.
type Source struct {
	cfg    Config
	stream accountactivity.SyncKey

	ready    bool
	currency num.Currency
	first    int64 // first ID this run may read
	last     int64 // last ID this run may read; < first means nothing
}

var _ accountactivity.Source = (*Source)(nil)

// NewSource validates cfg and returns a Source.
func NewSource(cfg Config) (*Source, error) {
	if cfg.Client == nil {
		return nil, errors.New("oanda: client is required")
	}
	if cfg.PageSize == 0 {
		cfg.PageSize = DefaultPageSize
	}
	if cfg.PageSize < 1 || cfg.PageSize > 1000 {
		return nil, fmt.Errorf("oanda: page size %d is outside 1..1000", cfg.PageSize)
	}
	if cfg.Bounds.set() {
		if cfg.Bounds.From.IsZero() {
			return nil, errors.New("oanda: bounds need a start (From); for everything, import unbounded")
		}
		if !cfg.Bounds.To.IsZero() && !cfg.Bounds.To.After(cfg.Bounds.From) {
			return nil, errors.New("oanda: bounds end must be after start")
		}
		if cfg.Bounds.To.IsZero() && cfg.Clock == nil {
			return nil, errors.New("oanda: bounds with no end need a clock for \"now\"")
		}
	}
	stream := accountactivity.SyncKey{Provider: Provider, Environment: cfg.Environment, ProviderAccountID: cfg.AccountID}
	if err := stream.Validate(); err != nil {
		return nil, fmt.Errorf("oanda: %w", err)
	}
	return &Source{cfg: cfg, stream: stream}, nil
}

// Stream implements accountactivity.Source.
func (s *Source) Stream() accountactivity.SyncKey { return s.stream }

func (s *Source) prepare(ctx context.Context) error {
	if s.ready {
		return nil
	}
	sum, err := s.cfg.Client.AccountSummary(ctx, s.cfg.AccountID)
	if err != nil {
		return err
	}
	cur, err := num.ParseCurrency(sum.Currency)
	if err != nil {
		return fmt.Errorf("%w: account currency %q", ErrBadResponse, sum.Currency)
	}
	s.currency = cur
	s.first, s.last = 1, sum.LastTransactionID

	if s.cfg.Bounds.set() {
		win, err := s.resolveBounds(ctx)
		if err != nil {
			return err
		}
		if win.Empty() {
			s.first, s.last = 1, 0
		} else {
			s.first = max(win.First, 1)
			s.last = min(win.Last, sum.LastTransactionID)
		}
	}
	s.ready = true
	return nil
}

// Next implements accountactivity.Source. The cursor is the last transaction
// ID already stored ("" for none); the page covers the next PageSize IDs.
func (s *Source) Next(ctx context.Context, cursor string) (accountactivity.Page, error) {
	var done int64
	if cursor != "" {
		var err error
		if done, err = parseID(cursor); err != nil {
			return accountactivity.Page{}, fmt.Errorf("oanda: checkpoint cursor: %w", err)
		}
	}
	if err := s.prepare(ctx); err != nil {
		return accountactivity.Page{}, err
	}

	start := max(done+1, s.first)
	if start > s.last {
		return accountactivity.Page{Cursor: cursor, Done: true}, nil
	}
	end := min(start+s.cfg.PageSize-1, s.last)

	raws, err := s.cfg.Client.IDRange(ctx, s.cfg.AccountID, start, end)
	if err != nil {
		return accountactivity.Page{}, err
	}
	acts := make([]accountactivity.Activity, 0, len(raws))
	for _, raw := range raws {
		a, err := normalize(raw, s.stream, s.currency)
		if err != nil {
			return accountactivity.Page{}, err
		}
		if id, err := parseID(a.ProviderActivityID); err != nil || id < start || id > end {
			return accountactivity.Page{}, fmt.Errorf("%w: transaction %q outside requested range %d-%d", ErrBadResponse, a.ProviderActivityID, start, end)
		}
		acts = append(acts, a)
	}
	return accountactivity.Page{
		Activities: acts,
		Cursor:     strconv.FormatInt(end, 10),
		Done:       end >= s.last,
	}, nil
}

// resolveBounds maps the half-open time window to the ID window it covers.
// The window is cut into spans OANDA accepts (365 days), each asked for with
// an inclusive end, and the ID windows are merged: IDs are sequential in
// time, so the first and last across all spans bound the whole window.
func (s *Source) resolveBounds(ctx context.Context) (IDWindow, error) {
	from := s.cfg.Bounds.From.UTC()
	var to time.Time // inclusive
	if s.cfg.Bounds.To.IsZero() {
		to = s.cfg.Clock.Now().UTC()
	} else {
		to = s.cfg.Bounds.To.UTC().Add(-time.Nanosecond)
	}
	merged := IDWindow{First: 1<<62 - 1, Last: -1}
	for start := from; !start.After(to); {
		end := start.Add(maxListSpan)
		if end.After(to) {
			end = to
		}
		win, err := s.cfg.Client.ResolveWindow(ctx, s.cfg.AccountID, start, end)
		if err != nil {
			return IDWindow{}, err
		}
		if !win.Empty() {
			merged.First = min(merged.First, win.First)
			merged.Last = max(merged.Last, win.Last)
		}
		start = end.Add(time.Nanosecond)
	}
	if merged.Last < 0 {
		return IDWindow{First: 1, Last: 0}, nil
	}
	return merged, nil
}
