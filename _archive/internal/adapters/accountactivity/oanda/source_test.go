package oanda

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/internal/accountactivity"
	"github.com/rustyeddy/trader/internal/clock"
)

func newSource(t *testing.T, f *fakeOANDA, mod ...func(*Config)) *Source {
	t.Helper()
	cfg := Config{Client: newClient(t, f), Environment: accountactivity.EnvironmentPractice, AccountID: testAccount, PageSize: 5}
	for _, m := range mod {
		m(&cfg)
	}
	s, err := NewSource(cfg)
	require.NoError(t, err)
	return s
}

func ids(p accountactivity.Page) []string {
	var out []string
	for _, a := range p.Activities {
		out = append(out, a.ProviderActivityID)
	}
	return out
}

func TestSource_PagesTheWholeHistoryByIDRange(t *testing.T) {
	f := loadFixture(t)
	s := newSource(t, f)
	assert.Equal(t, accountactivity.SyncKey{Provider: "oanda", Environment: accountactivity.EnvironmentPractice, ProviderAccountID: testAccount}, s.Stream())

	ctx := context.Background()
	p1, err := s.Next(ctx, "")
	require.NoError(t, err)
	assert.Equal(t, []string{"1", "2", "3", "4", "5"}, ids(p1))
	assert.Equal(t, "5", p1.Cursor)
	assert.False(t, p1.Done)

	p2, err := s.Next(ctx, p1.Cursor)
	require.NoError(t, err)
	assert.Equal(t, []string{"6", "7", "8", "9", "10"}, ids(p2))
	assert.Equal(t, "10", p2.Cursor)

	p3, err := s.Next(ctx, p2.Cursor)
	require.NoError(t, err)
	assert.Equal(t, []string{"11", "12"}, ids(p3))
	assert.Equal(t, "12", p3.Cursor)
	assert.True(t, p3.Done)

	assert.Equal(t, []string{"1-5", "6-10", "11-12"}, f.idranges())
}

func TestSource_ResumesFromTheCheckpointAndIsNothingNewWhenCaughtUp(t *testing.T) {
	f := loadFixture(t)
	ctx := context.Background()

	p, err := newSource(t, f).Next(ctx, "10")
	require.NoError(t, err)
	assert.Equal(t, []string{"11", "12"}, ids(p), "the page after the cursor, not from the start")

	caughtUp, err := newSource(t, f).Next(ctx, "12")
	require.NoError(t, err)
	assert.Empty(t, caughtUp.Activities)
	assert.Equal(t, "12", caughtUp.Cursor, "an unchanged cursor means nothing is newer")
	assert.True(t, caughtUp.Done)
}

// The same cursor yields the same page: a replay after any failure is safe.
func TestSource_NextIsAPureFunctionOfTheCursor(t *testing.T) {
	f := loadFixture(t)
	ctx := context.Background()
	a, err := newSource(t, f).Next(ctx, "5")
	require.NoError(t, err)
	b, err := newSource(t, f).Next(ctx, "5")
	require.NoError(t, err)
	assert.Equal(t, a, b)
}

func TestSource_BoundedHistoryReadsOnlyTheResolvedWindow(t *testing.T) {
	f := loadFixture(t)
	f.window = [][2]int64{{3, 7}, {8, 9}} // the time window covers IDs 3..9
	from := time.Date(2024, 3, 5, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 3, 7, 10, 0, 0, 0, time.UTC)
	ctx := context.Background()

	s := newSource(t, f, func(c *Config) { c.Bounds = Bounds{From: from, To: to}; c.PageSize = 100 })
	p, err := s.Next(ctx, "")
	require.NoError(t, err)
	assert.Equal(t, []string{"3", "4", "5", "6", "7", "8", "9"}, ids(p))
	assert.True(t, p.Done)
	assert.Equal(t, "9", p.Cursor)

	// A checkpoint from an earlier, narrower run starts at the window, not before it.
	s2 := newSource(t, f, func(c *Config) { c.Bounds = Bounds{From: from, To: to}; c.PageSize = 100 })
	p, err = s2.Next(ctx, "1")
	require.NoError(t, err)
	assert.Equal(t, "3", ids(p)[0])

	// A checkpoint already past the window means nothing to do.
	s3 := newSource(t, f, func(c *Config) { c.Bounds = Bounds{From: from, To: to} })
	p, err = s3.Next(ctx, "12")
	require.NoError(t, err)
	assert.Empty(t, p.Activities)
	assert.True(t, p.Done)

	// The window is sent to OANDA in RFC 3339 UTC.
	var listed bool
	for _, r := range f.requests {
		if strings.HasSuffix(r.URL.Path, "/transactions") {
			listed = true
			assert.Equal(t, "2024-03-05T00:00:00Z", r.URL.Query().Get("from"))
			assert.Equal(t, "2024-03-07T09:59:59.999999999Z", r.URL.Query().Get("to"),
				"OANDA's end is inclusive, so the half-open [From, To) asks for the instant before To")
		}
	}
	assert.True(t, listed)
}

func TestSource_EmptyWindowIsDoneWithNothing(t *testing.T) {
	f := loadFixture(t)
	f.window = nil
	s := newSource(t, f, func(c *Config) {
		c.Bounds = Bounds{From: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2030, 2, 1, 0, 0, 0, 0, time.UTC)}
	})
	p, err := s.Next(context.Background(), "")
	require.NoError(t, err)
	assert.Empty(t, p.Activities)
	assert.True(t, p.Done)
	assert.Empty(t, p.Cursor)
}

// A run reads a stable range even while the account keeps trading.
func TestSource_AStableEndWithinOneRun(t *testing.T) {
	f := loadFixture(t)
	ctx := context.Background()
	s := newSource(t, f, func(c *Config) { c.PageSize = 100 })
	p, err := s.Next(ctx, "")
	require.NoError(t, err)
	assert.True(t, p.Done)

	// Two more transactions arrive; the same Source still ends at 12...
	f.txs = append(f.txs, f.txs[0], f.txs[0])
	p, err = s.Next(ctx, "12")
	require.NoError(t, err)
	assert.True(t, p.Done)
	assert.Empty(t, p.Activities)
}

func TestSource_RejectsABadCursorAndBadConfig(t *testing.T) {
	f := loadFixture(t)
	_, err := newSource(t, f).Next(context.Background(), "not-an-id")
	assert.ErrorContains(t, err, "checkpoint cursor")
	_, err = newSource(t, f).Next(context.Background(), "-4")
	assert.Error(t, err)

	good := Config{Client: newClient(t, f), Environment: accountactivity.EnvironmentLive, AccountID: testAccount}
	for name, mod := range map[string]func(*Config){
		"no client":          func(c *Config) { c.Client = nil },
		"no environment":     func(c *Config) { c.Environment = accountactivity.EnvironmentUnknown },
		"no account":         func(c *Config) { c.AccountID = "" },
		"page too big":       func(c *Config) { c.PageSize = 1001 },
		"page negative":      func(c *Config) { c.PageSize = -1 },
		"inverted bounds":    func(c *Config) { c.Bounds = Bounds{From: time.Unix(200, 0), To: time.Unix(100, 0)} },
		"end without start":  func(c *Config) { c.Bounds = Bounds{To: time.Unix(100, 0)} },
		"open end, no clock": func(c *Config) { c.Bounds = Bounds{From: time.Unix(100, 0)} },
	} {
		t.Run(name, func(t *testing.T) {
			c := good
			mod(&c)
			_, err := NewSource(c)
			assert.Error(t, err)
		})
	}
}

func TestSource_FailuresAreClassifiedAndNeverExposeTheToken(t *testing.T) {
	ctx := context.Background()
	for status, want := range map[int]error{
		http.StatusUnauthorized:        ErrUnauthorized,
		http.StatusForbidden:           ErrUnauthorized,
		http.StatusBadRequest:          ErrBadRequest,
		http.StatusNotFound:            ErrBadRequest,
		http.StatusTooManyRequests:     ErrRateLimited,
		http.StatusInternalServerError: ErrProviderUnavailable,
		http.StatusTeapot:              ErrUnexpectedStatus,
	} {
		f := loadFixture(t)
		f.status = status
		_, err := newSource(t, f).Next(ctx, "")
		require.ErrorIs(t, err, want, "status %d", status)
		assert.NotContains(t, err.Error(), testToken)
	}

	f := loadFixture(t)
	f.badBody = `{not json`
	_, err := newSource(t, f).Next(ctx, "")
	assert.ErrorIs(t, err, ErrBadResponse)
}

func TestSource_RequestsCarryTheTokenAndDatetimeFormat(t *testing.T) {
	f := loadFixture(t)
	_, err := newSource(t, f).Next(context.Background(), "")
	require.NoError(t, err)
	require.NotEmpty(t, f.requests)
	for _, r := range f.requests {
		assert.Equal(t, "Bearer "+testToken, r.Header.Get("Authorization"))
		assert.Equal(t, "RFC3339", r.Header.Get("Accept-Datetime-Format"))
		assert.Equal(t, http.MethodGet, r.Method)
	}
}

func TestSource_APageWithAForeignOrOutOfRangeTransactionFails(t *testing.T) {
	f := loadFixture(t)
	// Serve transaction 12 where 1 should be: an ID outside the requested range.
	f.txs[0] = f.txs[11]
	_, err := newSource(t, f).Next(context.Background(), "")
	assert.ErrorIs(t, err, ErrBadResponse)
}

func TestBaseURL(t *testing.T) {
	u, err := BaseURL(accountactivity.EnvironmentPractice)
	require.NoError(t, err)
	assert.Equal(t, PracticeURL, u)
	u, err = BaseURL(accountactivity.EnvironmentLive)
	require.NoError(t, err)
	assert.Equal(t, LiveURL, u)
	_, err = BaseURL(accountactivity.EnvironmentUnknown)
	assert.Error(t, err)
	_, err = NewClient("", nil, nil)
	assert.Error(t, err)
}

// Bounds are half-open [From, To): a transaction exactly at To is not
// imported, and nothing is checkpointed past it.
func TestSource_AnExclusiveEndExcludesATransactionExactlyAtIt(t *testing.T) {
	f := loadFixture(t)
	f.byTime = true
	from := time.Date(2024, 3, 5, 0, 0, 0, 0, time.UTC)
	boundary := time.Date(2024, 3, 7, 8, 0, 0, 0, time.UTC) // transaction 8 is exactly here

	s := newSource(t, f, func(c *Config) { c.Bounds = Bounds{From: from, To: boundary}; c.PageSize = 100 })
	p, err := s.Next(context.Background(), "")
	require.NoError(t, err)
	assert.Equal(t, []string{"2", "3", "4", "5", "6", "7"}, ids(p), "transaction 8 is exactly at To and excluded")
	assert.Equal(t, "7", p.Cursor, "the checkpoint stops before the boundary transaction")

	// Moving To one nanosecond past it includes the transaction.
	f2 := loadFixture(t)
	f2.byTime = true
	s2 := newSource(t, f2, func(c *Config) { c.Bounds = Bounds{From: from, To: boundary.Add(time.Nanosecond)}; c.PageSize = 100 })
	p, err = s2.Next(context.Background(), "")
	require.NoError(t, err)
	assert.Contains(t, ids(p), "8")
}

// OANDA rejects list spans over 365 days: a long window is cut into spans of
// at most that, contiguous and non-overlapping, and the ID windows merged.
func TestSource_LongWindowsAreSplitIntoSupportedSpans(t *testing.T) {
	f := loadFixture(t)
	f.byTime = true
	from := time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 3, 6, 0, 0, 0, 0, time.UTC) // about five years

	s := newSource(t, f, func(c *Config) { c.Bounds = Bounds{From: from, To: to}; c.PageSize = 100 })
	p, err := s.Next(context.Background(), "")
	require.NoError(t, err)
	assert.Equal(t, []string{"1", "2", "3", "4", "5"}, ids(p), "transactions from every span are merged into one ID window")

	require.Greater(t, len(f.lists), 4, "five years need several requests")
	prevEnd := time.Time{}
	for i, l := range f.lists {
		assert.LessOrEqual(t, l.to.Sub(l.from), 365*24*time.Hour, "request %d exceeds OANDA's 365-day limit", i)
		if !prevEnd.IsZero() {
			assert.Equal(t, prevEnd.Add(time.Nanosecond), l.from, "spans are contiguous with no overlap and no gap")
		}
		prevEnd = l.to
	}
	assert.Equal(t, from, f.lists[0].from)
	assert.Equal(t, to.Add(-time.Nanosecond), f.lists[len(f.lists)-1].to)
}

// A start with no end means up to now, from the injected clock, and is split
// the same way.
func TestSource_AnOpenEndedWindowRunsUpToTheInjectedNow(t *testing.T) {
	f := loadFixture(t)
	f.byTime = true
	now := time.Date(2024, 3, 6, 12, 0, 0, 0, time.UTC)
	s := newSource(t, f, func(c *Config) {
		c.Bounds = Bounds{From: time.Date(2021, 3, 1, 0, 0, 0, 0, time.UTC)}
		c.Clock = clock.NewSimulated(now)
		c.PageSize = 100
	})
	p, err := s.Next(context.Background(), "")
	require.NoError(t, err)
	assert.Equal(t, []string{"1", "2", "3", "4", "5", "6", "7"}, ids(p))
	require.GreaterOrEqual(t, len(f.lists), 3)
	assert.Equal(t, now, f.lists[len(f.lists)-1].to)
	for _, l := range f.lists {
		assert.LessOrEqual(t, l.to.Sub(l.from), 365*24*time.Hour)
	}
}

// A list that reports matching transactions but links no pages is not an
// empty window: treating it as one would end the import without the history.
func TestSource_ACountWithNoPagesIsABadResponse(t *testing.T) {
	f := loadFixture(t)
	f.listBody = `{"count":10,"pages":[]}`
	s := newSource(t, f, func(c *Config) {
		c.Bounds = Bounds{From: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2024, 3, 8, 0, 0, 0, 0, time.UTC)}
	})
	_, err := s.Next(context.Background(), "")
	assert.ErrorIs(t, err, ErrBadResponse)

	f.listBody = `{"count":0,"pages":[]}`
	s2 := newSource(t, f, func(c *Config) {
		c.Bounds = Bounds{From: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2024, 3, 8, 0, 0, 0, 0, time.UTC)}
	})
	p, err := s2.Next(context.Background(), "")
	require.NoError(t, err, "a genuinely empty window is fine")
	assert.True(t, p.Done)
}

// A response without a transactions array must not look like a page with
// nothing in it: the source would advance past history it never fetched.
func TestSource_AMissingOrNullTransactionsArrayIsABadResponse(t *testing.T) {
	for name, body := range map[string]string{
		"empty object": `{}`, "null": `null`, "null array": `{"transactions":null}`, "wrong key": `{"txs":[]}`,
	} {
		t.Run(name, func(t *testing.T) {
			f := loadFixture(t)
			f.idrangeBody = body
			_, err := newSource(t, f).Next(context.Background(), "")
			assert.ErrorIs(t, err, ErrBadResponse)
		})
	}

	f := loadFixture(t)
	f.idrangeBody = `{"transactions":[]}`
	p, err := newSource(t, f).Next(context.Background(), "")
	require.NoError(t, err, "an explicit empty array is a legitimate empty page")
	assert.Empty(t, p.Activities)
	assert.Equal(t, "5", p.Cursor)
}

// The list response needs both count and pages; a body missing either must
// not read as a window with nothing in it, which would let a bounded import
// succeed having fetched none of the requested history.
func TestSource_AMissingOrNullListSchemaIsABadResponse(t *testing.T) {
	bounds := func(c *Config) {
		c.Bounds = Bounds{From: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2024, 3, 8, 0, 0, 0, 0, time.UTC)}
	}
	for name, body := range map[string]string{
		"empty object":      `{}`,
		"null":              `null`,
		"count only":        `{"count":0}`,
		"pages only":        `{"pages":[]}`,
		"null pages":        `{"pages":null}`,
		"zero, null pages":  `{"count":0,"pages":null}`,
		"null count":        `{"count":null,"pages":[]}`,
		"count and no list": `{"count":3}`,
	} {
		t.Run(name, func(t *testing.T) {
			f := loadFixture(t)
			f.listBody = body
			_, err := newSource(t, f, bounds).Next(context.Background(), "")
			assert.ErrorIs(t, err, ErrBadResponse)
		})
	}

	f := loadFixture(t)
	f.listBody = `{"count":0,"pages":[]}`
	p, err := newSource(t, f, bounds).Next(context.Background(), "")
	require.NoError(t, err, "the real empty response is a valid empty window")
	assert.Empty(t, p.Activities)
	assert.True(t, p.Done)
}
