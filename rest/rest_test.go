package rest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rustyeddy/trader/internal/store/storetest"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/provider/oanda"
	"github.com/rustyeddy/trader/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) { os.Exit(storetest.Main(m)) }

func utc(y int, m time.Month, d, h int) time.Time { return time.Date(y, m, d, h, 0, 0, 0, time.UTC) }

func px(s string) num.Price { return num.MustParsePrice(s) }

// fake is a ReadBarsFunc that records the request it got and returns what the test says.
type fake struct {
	got  []service.BarsRequest
	page service.BarsPage
	err  error
	do   func(ctx context.Context, req service.BarsRequest) (service.BarsPage, error)
}

func (f *fake) read(ctx context.Context, req service.BarsRequest) (service.BarsPage, error) {
	f.got = append(f.got, req)
	if f.do != nil {
		return f.do(ctx, req)
	}
	return f.page, f.err
}

func serve(t *testing.T, cfg Config, target string, header ...string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	rec := httptest.NewRecorder()
	NewHandler(cfg).ServeHTTP(rec, req)
	return rec
}

const goodQuery = BarsPath + "?instrument=EURUSD&interval=h1&from=2024-01-01T00:00:00Z&to=2024-02-01T00:00:00Z"

func samplePage() service.BarsPage {
	bid := marketdata.OHLC{Open: px("1.10000"), High: px("1.10100"), Low: px("1.09900"), Close: px("1.10050")}
	ask := marketdata.OHLC{Open: px("1.10010"), High: px("1.10110"), Low: px("1.09910"), Close: px("1.10060")}
	mid, _ := marketdata.MidOf(bid, ask)
	return service.BarsPage{
		Source: "oanda", Instrument: "EURUSD", Interval: "h1", From: utc(2024, 1, 1, 0), To: utc(2024, 2, 1, 0),
		Bars:     []marketdata.Bar{{Instrument: "EURUSD", Time: utc(2024, 1, 9, 0), AvailableAt: utc(2024, 1, 9, 1), Volume: 100, Bid: bid, Ask: ask, Mid: mid}},
		Coverage: service.BarsCoverage{Complete: true, Summary: service.CoverageSummary{Months: 1, Loaded: 1}},
	}
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &v), rec.Body.String())
	return v
}

func TestBars_ReturnsTheDocumentedSchemaWithExactPricesAndUTCTimes(t *testing.T) {
	f := &fake{page: samplePage()}

	rec := serve(t, Config{ReadBars: f.read}, goodQuery)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json; charset=utf-8", rec.Header().Get("Content-Type"))
	var raw map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &raw))
	assert.Equal(t, "oanda", raw["source"])
	assert.Equal(t, "2024-01-01T00:00:00Z", raw["from"])
	assert.NotContains(t, raw, "as_of", "left out when none was asked")
	assert.NotContains(t, raw, "next_cursor", "left out on the last page")
	bar := raw["bars"].([]any)[0].(map[string]any)
	assert.Equal(t, "2024-01-09T00:00:00Z", bar["time"])
	assert.Equal(t, "2024-01-09T01:00:00Z", bar["available_at"], "the opening time and the availability are both given")
	assert.Equal(t, float64(100), bar["volume"])
	assert.Equal(t, map[string]any{"open": "1.10000", "high": "1.10100", "low": "1.09900", "close": "1.10050"}, bar["bid"], "prices are exact decimal strings")
	assert.Contains(t, bar, "ask")
	assert.Contains(t, bar, "mid")
	assert.NotContains(t, bar, "trade", "a price basis the bar does not have is left out")
	cov := raw["coverage"].(map[string]any)
	assert.Equal(t, true, cov["complete"])
	assert.Equal(t, []any{}, cov["months"], "empty lists, never null")
	assert.Equal(t, []any{}, cov["gaps"])
}

func TestBars_NormalizesParametersIntoTheServiceRequest(t *testing.T) {
	f := &fake{page: samplePage()}
	q := BarsPath + "?instrument=eurusd&interval=H1&source=OANDA&from=2024-01-01&to=2024-02-01T01:00:00%2B01:00&as_of=2024-01-15T00:00:00Z&limit=250"

	rec := serve(t, Config{ReadBars: f.read}, q)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, f.got, 1)
	assert.Equal(t, service.BarsRequest{Source: "oanda", Instrument: "EURUSD", Interval: "h1",
		From: utc(2024, 1, 1, 0), To: utc(2024, 2, 1, 0), AsOf: utc(2024, 1, 15, 0), Limit: 250, Strict: false}, f.got[0],
		"names are normalized, an offset becomes UTC, a bare date is midnight UTC, and partial is the default")
}

func TestBars_PartialFalseAsksTheServiceForStrict(t *testing.T) {
	f := &fake{page: samplePage()}

	serve(t, Config{ReadBars: f.read}, goodQuery+"&partial=false")
	serve(t, Config{ReadBars: f.read}, goodQuery+"&partial=true")

	require.Len(t, f.got, 2)
	assert.True(t, f.got[0].Strict)
	assert.False(t, f.got[1].Strict)
}

func TestBars_AsOfIsEchoedAndANextCursorContinuesTheSameQueryOnly(t *testing.T) {
	f := &fake{}
	f.do = func(_ context.Context, req service.BarsRequest) (service.BarsPage, error) {
		p := samplePage()
		p.AsOf = req.AsOf
		if req.After.IsZero() {
			p.NextAfter = utc(2024, 1, 9, 0)
		}
		return p, nil
	}
	cfg := Config{ReadBars: f.read}
	q := goodQuery + "&as_of=2024-01-20T00:00:00Z&limit=1"

	first := decode[map[string]any](t, serve(t, cfg, q))
	cursor, _ := first["next_cursor"].(string)
	require.NotEmpty(t, cursor)
	assert.Equal(t, "2024-01-20T00:00:00Z", first["as_of"])

	second := serve(t, cfg, q+"&cursor="+url.QueryEscape(cursor))
	require.Equal(t, http.StatusOK, second.Code)
	require.Len(t, f.got, 2)
	assert.Equal(t, utc(2024, 1, 9, 0), f.got[1].After, "the cursor carries the position to the service")
	assert.NotContains(t, decode[map[string]any](t, second), "next_cursor")

	otherQuery := serve(t, cfg, strings.Replace(q, "EURUSD", "GBPUSD", 1)+"&cursor="+url.QueryEscape(cursor))
	assert.Equal(t, http.StatusBadRequest, otherQuery.Code)
	assert.Equal(t, "invalid_cursor", decode[errorBody](t, otherQuery).Error.Code)
	assert.Contains(t, decode[errorBody](t, otherQuery).Error.Message, "different query")
	assert.Len(t, f.got, 2, "the service is not asked")
}

func TestBars_ALimitChangeBetweenPagesIsAllowedButAnyOtherChangeIsNot(t *testing.T) {
	f := &fake{}
	f.do = func(_ context.Context, req service.BarsRequest) (service.BarsPage, error) {
		p := samplePage()
		p.NextAfter = utc(2024, 1, 9, 0)
		return p, nil
	}
	cfg := Config{ReadBars: f.read}
	first := decode[map[string]any](t, serve(t, cfg, goodQuery+"&limit=1"))
	cursor := url.QueryEscape(first["next_cursor"].(string))

	biggerPage := serve(t, cfg, goodQuery+"&limit=500&cursor="+cursor)
	strict := serve(t, cfg, goodQuery+"&partial=false&cursor="+cursor)

	assert.Equal(t, http.StatusOK, biggerPage.Code)
	assert.Equal(t, http.StatusBadRequest, strict.Code, "strict and partial are different queries")
}

func TestBars_AnEmptyPageHasEmptyListsNotNulls(t *testing.T) {
	page := samplePage()
	page.Bars = nil
	f := &fake{page: page}

	rec := serve(t, Config{ReadBars: f.read}, goodQuery)

	assert.Contains(t, rec.Body.String(), `"bars":[]`)
}

func TestBars_ClientErrorsNameTheParameterAndNeverReachTheService(t *testing.T) {
	tests := map[string]string{
		"no instrument":             BarsPath + "?interval=h1&from=2024-01-01&to=2024-02-01",
		"no interval":               BarsPath + "?instrument=EURUSD&from=2024-01-01&to=2024-02-01",
		"no from":                   BarsPath + "?instrument=EURUSD&interval=h1&to=2024-02-01",
		"no to":                     BarsPath + "?instrument=EURUSD&interval=h1&from=2024-01-01",
		"a bad from":                goodQuery + "&from=yesterday",
		"a bad to":                  strings.Replace(goodQuery, "to=2024-02-01T00:00:00Z", "to=31-01-2024", 1),
		"a bad as_of":               goodQuery + "&as_of=noon",
		"limit zero":                goodQuery + "&limit=0",
		"limit too big":             goodQuery + "&limit=" + strconv.Itoa(service.MaxBarsLimit+1),
		"limit not a number":        goodQuery + "&limit=many",
		"partial not bool":          goodQuery + "&partial=maybe",
		"an unknown param":          goodQuery + "&format=csv",
		"a repeated param":          goodQuery + "&instrument=GBPUSD",
		"a garbage cursor":          goodQuery + "&cursor=!!!",
		"a cursor that is not ours": goodQuery + "&cursor=" + "e30",
	}
	for name, target := range tests {
		t.Run(name, func(t *testing.T) {
			f := &fake{page: samplePage()}

			rec := serve(t, Config{ReadBars: f.read}, target)

			require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			body := decode[errorBody](t, rec)
			assert.NotEmpty(t, body.Error.Code)
			assert.NotEmpty(t, body.Error.Message)
			assert.Empty(t, f.got, "a bad request is refused before any data is read")
		})
	}
}

func TestBars_ServiceErrorsBecomePredictableStatuses(t *testing.T) {
	coverage := service.BarsCoverage{Summary: service.CoverageSummary{Months: 2, Missing: 2},
		Months: []service.MonthStatus{{Year: 2024, Month: time.January, State: service.MonthMissing}}}
	tests := []struct {
		name       string
		err        error
		status     int
		code       string
		hasCovered bool
	}{
		{"an invalid request", errors.Join(service.ErrInvalidBarsRequest, errors.New("limit")), http.StatusBadRequest, "invalid_request", false},
		{"no data", service.ErrNoData, http.StatusNotFound, "no_data", true},
		{"an incomplete range in strict mode", service.ErrBarsIncomplete, http.StatusUnprocessableEntity, "incomplete", true},
		{"a timeout", context.DeadlineExceeded, http.StatusGatewayTimeout, "timeout", false},
		{"a cancellation", context.Canceled, 499, "cancelled", false},
		{"anything else", errors.New("open /srv/data/secret/path: permission denied"), http.StatusInternalServerError, "internal", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := &fake{page: service.BarsPage{Coverage: coverage}, err: tc.err}

			rec := serve(t, Config{ReadBars: f.read}, goodQuery)

			assert.Equal(t, tc.status, rec.Code)
			body := decode[errorBody](t, rec)
			assert.Equal(t, tc.code, body.Error.Code)
			assert.Equal(t, tc.hasCovered, body.Coverage != nil, "what is missing is described when the range is empty or incomplete")
			assert.NotContains(t, rec.Body.String(), "/srv/data", "an internal failure never leaks a path or its detail")
			assert.NotContains(t, rec.Body.String(), "permission denied")
		})
	}
}

func TestBars_AServiceThatTakesTooLongIsCutOff(t *testing.T) {
	f := &fake{do: func(ctx context.Context, _ service.BarsRequest) (service.BarsPage, error) {
		<-ctx.Done()
		return service.BarsPage{}, ctx.Err()
	}}

	rec := serve(t, Config{ReadBars: f.read, Timeout: 20 * time.Millisecond}, goodQuery)

	assert.Equal(t, http.StatusGatewayTimeout, rec.Code)
	assert.Equal(t, "timeout", decode[errorBody](t, rec).Error.Code)
}

func TestHandler_OtherMethodsAndPathsGetJSONErrors(t *testing.T) {
	f := &fake{page: samplePage()}
	h := NewHandler(Config{ReadBars: f.read})

	post := httptest.NewRecorder()
	h.ServeHTTP(post, httptest.NewRequest(http.MethodPost, goodQuery, nil))
	missing := httptest.NewRecorder()
	h.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/v1/marketdata/everything", nil))

	assert.Equal(t, http.StatusMethodNotAllowed, post.Code)
	assert.Equal(t, "GET", post.Header().Get("Allow"))
	assert.Equal(t, "method_not_allowed", decode[errorBody](t, post).Error.Code)
	assert.Equal(t, http.StatusNotFound, missing.Code)
	assert.Equal(t, "not_found", decode[errorBody](t, missing).Error.Code)
	assert.Empty(t, f.got)
}

func TestHandler_ATokenWhenConfiguredIsRequiredAndNeverEchoed(t *testing.T) {
	const token = "s3cret-token-value"
	f := &fake{page: samplePage()}
	cfg := Config{ReadBars: f.read, Token: token}

	none := serve(t, cfg, goodQuery)
	wrong := serve(t, cfg, goodQuery, "Authorization", "Bearer nope")
	wrongScheme := serve(t, cfg, goodQuery, "Authorization", "Basic "+token)
	right := serve(t, cfg, goodQuery, "Authorization", "Bearer "+token)
	notFound := serve(t, cfg, "/nothing")

	for _, rec := range []*httptest.ResponseRecorder{none, wrong, wrongScheme} {
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
		assert.Equal(t, `Bearer realm="trader"`, rec.Header().Get("WWW-Authenticate"))
		assert.Equal(t, "unauthorized", decode[errorBody](t, rec).Error.Code)
		assert.NotContains(t, rec.Body.String(), token)
	}
	assert.Equal(t, http.StatusOK, right.Code)
	assert.Equal(t, http.StatusUnauthorized, notFound.Code, "no endpoint, not even a 404, is open without the token")
	assert.Len(t, f.got, 1, "only the authenticated request reached the service")
}

// --- end to end: real service over canonical files, no live credentials ---

func writeRaw(t *testing.T, recs ...oanda.Record) {
	t.Helper()
	key := oanda.PartitionKey{Symbol: "EURUSD", Interval: oanda.RawH1, Year: 2024, Month: time.January}
	require.NoError(t, oanda.WritePartition(context.Background(), key, recs, true))
	require.NoError(t, service.BuildCanonical(context.Background(), nil,
		service.BuildRequest{Instruments: []string{"EURUSD"}, Interval: "h1", From: utc(2024, 1, 1, 0), To: utc(2024, 2, 1, 0)}, nil))
}

func rawRec(at time.Time) oanda.Record {
	return oanda.Record{Time: at, BidOpen: px("1.10000"), BidHigh: px("1.10100"), BidLow: px("1.09900"), BidClose: px("1.10050"),
		AskOpen: px("1.10010"), AskHigh: px("1.10110"), AskLow: px("1.09910"), AskClose: px("1.10060"), Volume: 100, Complete: true}
}

func realHandler() http.Handler {
	return NewHandler(Config{ReadBars: func(ctx context.Context, req service.BarsRequest) (service.BarsPage, error) {
		return service.ReadBars(ctx, nil, req)
	}})
}

func get(t *testing.T, h http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func TestEndToEnd_PagesOverHTTPNeitherRepeatNorOmitABarAndTheGapIsListedOnce(t *testing.T) {
	storetest.Root(t)
	var recs []oanda.Record
	for h := range 24 {
		if h == 10 { // a missing hour on a Tuesday that nothing explains
			continue
		}
		recs = append(recs, rawRec(utc(2024, 1, 9, h)))
	}
	writeRaw(t, recs...)
	h := realHandler()
	base := BarsPath + "?instrument=EURUSD&interval=h1&from=2024-01-09T00:00:00Z&to=2024-01-10T00:00:00Z&limit=6"

	var times []string
	var gaps []gapJSON
	target := base
	pages := 0
	for {
		rec := get(t, h, target)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		page := decode[barsJSON](t, rec)
		pages++
		for _, b := range page.Bars {
			times = append(times, b.Time.Format(time.RFC3339))
		}
		gaps = append(gaps, page.Coverage.Gaps...)
		if page.NextCursor == "" {
			break
		}
		require.Less(t, pages, 20)
		target = base + "&cursor=" + url.QueryEscape(page.NextCursor)
	}

	assert.Len(t, times, 23, "every stored bar, once")
	for i := 1; i < len(times); i++ {
		assert.Less(t, times[i-1], times[i], "ascending, no duplicates")
	}
	assert.NotContains(t, times, "2024-01-09T10:00:00Z")
	assert.Equal(t, 4, pages, "23 bars in pages of 6")
	require.Len(t, gaps, 1)
	assert.Equal(t, "unexplained", gaps[0].Kind)
	assert.Equal(t, utc(2024, 1, 9, 10), gaps[0].Start)
	assert.Equal(t, utc(2024, 1, 9, 11), gaps[0].End)
}

func TestEndToEnd_APartialRangeIsServedAndStrictRefusesItWithTheSameCoverage(t *testing.T) {
	storetest.Root(t)
	writeRaw(t, rawRec(utc(2024, 1, 9, 0)), rawRec(utc(2024, 1, 9, 1)))
	h := realHandler()
	q := BarsPath + "?instrument=EURUSD&interval=h1&from=2024-01-01&to=2024-03-01" // February is missing

	partial := get(t, h, q)
	strict := get(t, h, q+"&partial=false")

	require.Equal(t, http.StatusOK, partial.Code, "partial is the default")
	page := decode[barsJSON](t, partial)
	assert.Len(t, page.Bars, 2)
	assert.False(t, page.Coverage.Complete)
	assert.Equal(t, 1, page.Coverage.Summary.Missing)
	require.Len(t, page.Coverage.Months, 1)
	assert.Equal(t, "missing", page.Coverage.Months[0].State)
	assert.Equal(t, 2, page.Coverage.Months[0].Month)
	require.Equal(t, http.StatusUnprocessableEntity, strict.Code)
	body := decode[errorBody](t, strict)
	assert.Equal(t, "incomplete", body.Error.Code)
	require.NotNil(t, body.Coverage)
	assert.Equal(t, page.Coverage, *body.Coverage, "the same description of what is missing")
}

func TestEndToEnd_AnUnknownInstrumentIsNoDataAndAnInvalidOneIsABadRequest(t *testing.T) {
	storetest.Root(t)
	writeRaw(t, rawRec(utc(2024, 1, 9, 0)))
	h := realHandler()

	unknown := get(t, h, BarsPath+"?instrument=NOPE&interval=h1&from=2024-01-01&to=2024-02-01")
	path := get(t, h, BarsPath+"?instrument=..%2Fx&interval=h1&from=2024-01-01&to=2024-02-01")
	badInterval := get(t, h, BarsPath+"?instrument=EURUSD&interval=h4&from=2024-01-01&to=2024-02-01")
	reversed := get(t, h, BarsPath+"?instrument=EURUSD&interval=h1&from=2024-02-01&to=2024-01-01")

	assert.Equal(t, http.StatusNotFound, unknown.Code)
	assert.Equal(t, "no_data", decode[errorBody](t, unknown).Error.Code)
	for _, rec := range []*httptest.ResponseRecorder{path, badInterval, reversed} {
		assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		assert.Equal(t, "invalid_request", decode[errorBody](t, rec).Error.Code)
	}
}

func TestEndToEnd_AsOfLeavesOutABarThatIsNotYetAvailable(t *testing.T) {
	storetest.Root(t)
	var recs []oanda.Record
	for h := range 6 {
		recs = append(recs, rawRec(utc(2024, 1, 9, h)))
	}
	writeRaw(t, recs...)
	h := realHandler()

	rec := get(t, h, BarsPath+"?instrument=EURUSD&interval=h1&from=2024-01-09&to=2024-01-10&as_of=2024-01-09T03:00:00Z")

	require.Equal(t, http.StatusOK, rec.Code)
	page := decode[barsJSON](t, rec)
	require.Len(t, page.Bars, 3, "the 03:00 bar closes at 04:00: not yet usable at 03:00")
	for _, b := range page.Bars {
		assert.False(t, b.AvailableAt.After(utc(2024, 1, 9, 3)))
	}
	require.NotNil(t, page.AsOf)
}

// --- the boundary ---

// TestRest_ImportsOnlyTheServiceAndThePublicModel keeps the transport thin and
// provider-free (ADR-091, #680): within the module a non-test file may import only
// the application service and the public market-data model.
func TestRest_ImportsOnlyTheServiceAndThePublicModel(t *testing.T) {
	const mod = "github.com/rustyeddy/trader"
	allowed := map[string]bool{mod + "/service": true, mod + "/marketdata": true}
	files, err := os.ReadDir(".")
	require.NoError(t, err)
	checked := 0
	for _, e := range files {
		if !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		checked++
		f, perr := parser.ParseFile(token.NewFileSet(), e.Name(), nil, parser.ImportsOnly)
		require.NoError(t, perr)
		for _, imp := range f.Imports {
			path, uerr := strconv.Unquote(imp.Path.Value)
			require.NoError(t, uerr)
			if path == mod || strings.HasPrefix(path, mod+"/") {
				assert.True(t, allowed[path], "%s imports %s", e.Name(), path)
			}
			assert.NotContains(t, path, "/provider/", "no provider in the transport")
		}
	}
	assert.Positive(t, checked)
}

func TestBars_TradeDataIsDescribedAndQuoteDataIsNot(t *testing.T) {
	page := samplePage()
	plain := decode[map[string]any](t, serve(t, Config{ReadBars: (&fake{page: page}).read}, goodQuery))
	assert.NotContains(t, plain, "data")

	page.Data = &service.DataInfo{AvailabilityPolicy: "p", Calendar: "c", PriceBasis: "trade", Currency: "USD", VolumeUnit: "shares",
		Adjustment: "total-return", AdjustmentEvidence: "observed", SnapshotFingerprint: "sha256:ab", SnapshotAsOf: "2026-09-10"}
	got := decode[map[string]any](t, serve(t, Config{ReadBars: (&fake{page: page}).read}, goodQuery))

	assert.Equal(t, map[string]any{"availability_policy": "p", "calendar": "c", "price_basis": "trade", "currency": "USD",
		"volume_unit": "shares", "adjustment": "total-return", "adjustment_evidence": "observed",
		"snapshot_fingerprint": "sha256:ab", "snapshot_as_of": "2026-09-10"}, got["data"])
}

func TestBars_AMixedSeriesIsItsOwnError(t *testing.T) {
	f := &fake{do: func(context.Context, service.BarsRequest) (service.BarsPage, error) {
		return service.BarsPage{}, fmt.Errorf("%w: two snapshots", service.ErrBarsMixed)
	}}

	rec := serve(t, Config{ReadBars: f.read}, goodQuery)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Equal(t, "mixed_series", decode[errorBody](t, rec).Error.Code)
}
