package account

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/cmd/internal/accountactivitycfg"
	"github.com/rustyeddy/trader/cmd/internal/marketdatacfg"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/internal/accountactivity"
	"github.com/rustyeddy/trader/internal/adapters/accountactivity/filestore"
	"github.com/rustyeddy/trader/internal/clock"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

var update = flag.Bool("update", false, "rewrite golden files")

var (
	usd    = num.MustParseCurrency("USD")
	t0     = time.Date(2024, 3, 1, 9, 0, 0, 0, time.UTC)
	syncAt = time.Date(2024, 4, 1, 12, 0, 0, 0, time.UTC)
	eurusd = instrument.CurrencyPairID(num.MustParseCurrency("EUR"), usd)
	stream = accountactivity.SyncKey{Provider: "oanda", Environment: accountactivity.EnvironmentPractice, ProviderAccountID: "acct"}
)

func TestMain(m *testing.M) {
	// Hermetic: never read a developer's or a server's real config file.
	marketdatacfg.DefaultConfigPath = ""
	os.Exit(m.Run())
}

func day(n int) time.Duration { return time.Duration(n) * 24 * time.Hour }

type fillSpec struct {
	id                      string
	at                      time.Duration
	side                    order.Side
	qty, price, vwap        string
	pl, comm, fin, spr, fee string // "" leaves the figure unknown
}

func fill(s fillSpec) accountactivity.Activity {
	a := accountactivity.Activity{
		Provider: stream.Provider, Environment: stream.Environment, ProviderAccountID: stream.ProviderAccountID,
		ProviderActivityID: s.id, ProviderType: "ORDER_FILL", Kind: accountactivity.KindFill,
		OccurredAt: t0.Add(s.at), Instrument: eurusd, Side: s.side,
		Quantity: accountactivity.Reported(num.MustParseQuantity(s.qty)),
		Price:    accountactivity.Reported(num.MustParsePrice(s.price)),
	}
	if s.vwap != "" {
		a.FullVWAP = accountactivity.Reported(num.MustParsePrice(s.vwap))
	}
	for _, f := range []struct {
		dst *accountactivity.Value[num.Money]
		src string
	}{{&a.RealizedPL, s.pl}, {&a.Commission, s.comm}, {&a.Financing, s.fin}, {&a.SpreadCost, s.spr}, {&a.OtherFees, s.fee}} {
		if f.src != "" {
			*f.dst = accountactivity.Reported(num.MustParseMoney(f.src, usd))
		}
	}
	return a
}

// withEffect records the broker's per-trade fact for the fill (trade t1), so
// the execution price is the units-weighted per-trade price.
func withEffect(a accountactivity.Activity, role accountactivity.TradeRole, units, price string) accountactivity.Activity {
	a.TradeEffects = []accountactivity.TradeEffect{{
		BrokerTradeID: "t1", Role: role,
		Units: accountactivity.Reported(num.MustParseQuantity(units)), Price: accountactivity.Reported(num.MustParsePrice(price)),
	}}
	return a
}

func deposit(id string, at time.Duration, amount string) accountactivity.Activity {
	return accountactivity.Activity{
		Provider: stream.Provider, Environment: stream.Environment, ProviderAccountID: stream.ProviderAccountID,
		ProviderActivityID: id, ProviderType: "TRANSFER_FUNDS", Kind: accountactivity.KindDeposit,
		OccurredAt: t0.Add(at), Cash: accountactivity.Reported(num.MustParseMoney(amount, usd)),
	}
}

// fixture: a deposit; trade A (closed day 2: known zero financing, unknown
// spread cost); trade B (closed day 6, broker fills exactly at fullVWAP); and
// an open position C.
func fixture() []accountactivity.Activity {
	return []accountactivity.Activity{
		deposit("1", 0, "10000"),
		withEffect(fill(fillSpec{id: "2", at: time.Hour, side: order.Buy, qty: "1000", price: "1.1010", vwap: "1.1000"}), accountactivity.TradeRoleOpened, "1000", "1.1010"),
		withEffect(fill(fillSpec{id: "3", at: day(2), side: order.Sell, qty: "1000", price: "1.1090", vwap: "1.1100", pl: "8", comm: "-1", fin: "0"}), accountactivity.TradeRoleClosed, "1000", "1.1090"),
		fill(fillSpec{id: "4", at: day(5), side: order.Buy, qty: "500", price: "1.1200", vwap: "1.1200", comm: "-0.5", spr: "0.15", fee: "0"}),
		fill(fillSpec{id: "5", at: day(6), side: order.Sell, qty: "500", price: "1.1300", vwap: "1.1300", pl: "5", comm: "-0.5", fin: "-0.25", spr: "0.15", fee: "0"}),
		fill(fillSpec{id: "6", at: day(8), side: order.Buy, qty: "200", price: "1.1400"}),
	}
}

// scripted is a Source returning fixed pages, standing in for a provider.
type scripted struct {
	pages map[string]accountactivity.Page
	calls int
}

func (s *scripted) Stream() accountactivity.SyncKey { return stream }
func (s *scripted) Next(_ context.Context, cursor string) (accountactivity.Page, error) {
	s.calls++
	p, ok := s.pages[cursor]
	if !ok {
		return accountactivity.Page{Cursor: cursor, Done: true}, nil
	}
	acts := make([]accountactivity.Activity, len(p.Activities))
	for i, a := range p.Activities {
		a.ImportedAt = time.Time{}
		acts[i] = a
	}
	p.Activities = acts
	return p, nil
}

type env struct {
	root string
	src  *scripted
	d    deps
}

func newEnv(t *testing.T) *env { return newEnvWith(t, fixture()) }

// newEnvWith serves the given activities as the broker's history, in two
// pages, with the checkpoint cursor "end" after the last.
func newEnvWith(t *testing.T, acts []accountactivity.Activity) *env {
	t.Helper()
	root := t.TempDir()
	e := &env{root: root}
	half := len(acts) / 2
	e.src = &scripted{pages: map[string]accountactivity.Page{
		"":    {Activities: acts[:half], Cursor: "mid"},
		"mid": {Activities: acts[half:], Cursor: "end", Done: true},
	}}
	e.d = deps{
		environ: func() []string { return []string{"TRADER_ACCOUNTACTIVITY_ROOT=" + root} },
		openStore: func(c accountactivitycfg.Config) (accountactivity.Store, error) {
			r, err := c.Root()
			if err != nil {
				return nil, err
			}
			return filestore.New(r)
		},
		newSource: func(accountactivitycfg.Config, accountactivity.SyncKey, accountactivitycfg.Bounds, clock.Clock) (accountactivity.Source, error) {
			return e.src, nil
		},
		clock: clock.NewSimulated(syncAt),
	}
	return e
}

// run executes the account command tree with the standard stream flags.
func (e *env) run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	return e.runRaw(t, append([]string{"--environment", "practice", "--account", "acct"}, args...)...)
}

func (e *env) runRaw(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newCommand(e.d)
	var out, errb bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errb)
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(context.Background())
	return out.String(), err
}

func (e *env) synced(t *testing.T) *env {
	t.Helper()
	_, err := e.run(t, "activity", "sync")
	require.NoError(t, err)
	return e
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		require.NoError(t, os.MkdirAll("testdata", 0o755))
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
		return
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err, "run with -update to create %s", path)
	assert.Equal(t, string(want), got, name)
}

func decode(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(s), &m), s)
	return m
}

// ---- sync ----

func TestSync_ImportsAndIsIdempotent(t *testing.T) {
	e := newEnv(t)
	out, err := e.run(t, "activity", "sync")
	require.NoError(t, err)
	golden(t, "sync_first.golden", out)

	out, err = e.run(t, "activity", "sync")
	require.NoError(t, err)
	assert.Regexp(t, `new:\s+0`, out, "a rerun adds nothing")
	assert.Regexp(t, `cursor:\s+end`, out)

	out, err = e.run(t, "activity", "sync", "--format", "json")
	require.NoError(t, err)
	golden(t, "sync_rerun.json.golden", out)
	m := decode(t, out)
	assert.EqualValues(t, 1, m["schema_version"])
	assert.EqualValues(t, 0, m["new"])
}

func TestSync_FailureIsNonZeroAndReportsProgress(t *testing.T) {
	e := newEnv(t)
	e.d.newSource = func(accountactivitycfg.Config, accountactivity.SyncKey, accountactivitycfg.Bounds, clock.Clock) (accountactivity.Source, error) {
		return nil, assert.AnError
	}
	_, err := e.run(t, "activity", "sync")
	assert.ErrorIs(t, err, assert.AnError)

	// A bounded import needs a start.
	_, err = newEnv(t).run(t, "activity", "sync", "--to", "2024-03-02")
	assert.ErrorContains(t, err, "--to needs --from")
}

func TestSync_PassesBoundsToTheSource(t *testing.T) {
	e := newEnv(t)
	var got accountactivitycfg.Bounds
	e.d.newSource = func(_ accountactivitycfg.Config, _ accountactivity.SyncKey, b accountactivitycfg.Bounds, _ clock.Clock) (accountactivity.Source, error) {
		got = b
		return e.src, nil
	}
	_, err := e.run(t, "activity", "sync", "--from", "2024-03-01", "--to", "2024-03-05T00:00:00Z")
	require.NoError(t, err)
	assert.Equal(t, time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC), got.From)
	assert.Equal(t, time.Date(2024, 3, 5, 0, 0, 0, 0, time.UTC), got.To)
}

// ---- list ----

func TestList_TableAndJSONGolden(t *testing.T) {
	e := newEnv(t).synced(t)
	out, err := e.run(t, "activity", "list")
	require.NoError(t, err)
	golden(t, "list.golden", out)

	out, err = e.run(t, "activity", "list", "--format", "json")
	require.NoError(t, err)
	golden(t, "list.json.golden", out)
}

func TestList_UnknownIsNotZero(t *testing.T) {
	e := newEnv(t).synced(t)
	out, err := e.run(t, "activity", "list", "--kind", "fill")
	require.NoError(t, err)

	// Fill 2 supplied nothing: every cost is unknown. Fill 3 reported a zero
	// financing and an unknown spread cost.
	var line2, line3 string
	for _, l := range strings.Split(out, "\n") {
		switch {
		case strings.Contains(l, " 2  ") && strings.Contains(l, "ORDER_FILL"):
			line2 = l
		case strings.Contains(l, " 3  ") && strings.Contains(l, "ORDER_FILL"):
			line3 = l
		}
	}
	require.NotEmpty(t, line2)
	require.NotEmpty(t, line3)
	assert.Contains(t, line2, "commission=unknown")
	assert.Contains(t, line3, "financing=0.00 USD", "a reported zero is a real zero")
	assert.Contains(t, line3, "spread_cost=unknown")
	assert.NotContains(t, out, "financing=unknown USD")
}

func TestList_RangeKindAndLimit(t *testing.T) {
	e := newEnv(t).synced(t)

	out, err := e.run(t, "activity", "list", "--from", "2024-03-02", "--to", "2024-03-07", "--format", "json")
	require.NoError(t, err)
	m := decode(t, out)
	assert.EqualValues(t, 2, m["matched"], "fills 3 and 4 fall in [03-02, 03-07); fill 5 is at 09:00 on 03-07")
	assert.Equal(t, false, m["truncated"])

	out, err = e.run(t, "activity", "list", "--limit", "2", "--format", "json")
	require.NoError(t, err)
	m = decode(t, out)
	assert.Len(t, m["activities"], 2)
	assert.Equal(t, true, m["truncated"])
	assert.EqualValues(t, 6, m["matched"])

	out, err = e.run(t, "activity", "list", "--limit", "2")
	require.NoError(t, err)
	assert.Contains(t, out, "showing the first 2 of 6 matching activities")

	out, err = e.run(t, "activity", "list", "--kind", "deposit", "--format", "json")
	require.NoError(t, err)
	assert.Len(t, decode(t, out)["activities"], 1)

	out, err = e.run(t, "activity", "list", "--from", "2030-01-01")
	require.NoError(t, err)
	assert.Contains(t, out, "no activity in range")
	out, err = e.run(t, "activity", "list", "--from", "2030-01-01", "--format", "json")
	require.NoError(t, err)
	assert.Equal(t, []any{}, decode(t, out)["activities"], "[] and never null")

	for _, bad := range [][]string{
		{"--kind", "nonsense"}, {"--limit", "-1"}, {"--from", "yesterday"},
		{"--from", "2024-03-05", "--to", "2024-03-01"}, {"--from", "2024-03-05", "--to", "2024-03-05"},
	} {
		_, err = e.run(t, append([]string{"activity", "list"}, bad...)...)
		assert.Error(t, err, bad)
	}
}

// ---- trades ----

func TestTrades_TableAndJSONGolden(t *testing.T) {
	e := newEnv(t).synced(t)
	for name, args := range map[string][]string{
		"trades.golden":                {"trades"},
		"trades.json.golden":           {"trades", "--format", "json"},
		"trades_open.golden":           {"trades", "--open"},
		"trades_open.json.golden":      {"trades", "--open", "--format", "json"},
		"trades_benchmark.golden":      {"trades", "--benchmark", "broker-vwap"},
		"trades_benchmark.json.golden": {"trades", "--benchmark", "broker-vwap", "--format", "json"},
	} {
		out, err := e.run(t, args...)
		require.NoError(t, err, name)
		golden(t, name, out)
	}
}

func TestTrades_UnknownVersusZeroInTheTable(t *testing.T) {
	e := newEnv(t).synced(t)
	out, err := e.run(t, "trades")
	require.NoError(t, err)
	lines := strings.Split(out, "\n")
	var a, b string
	for _, l := range lines {
		if strings.Contains(l, "/3#long") || strings.Contains(l, "/2#long") {
			a = l
		}
		if strings.Contains(l, "/4#long") {
			b = l
		}
	}
	require.NotEmpty(t, a)
	require.NotEmpty(t, b)
	// Trade A reported zero financing and no spread cost: 0.00 versus unknown.
	assert.Contains(t, a, "0.00 USD")
	assert.Contains(t, a, "unknown")
	// Trade B reported its spread cost.
	assert.Contains(t, b, "0.30 USD")
}

func TestTrades_NoBenchmarkMeansNoSlippageAnywhere(t *testing.T) {
	e := newEnv(t).synced(t)
	out, err := e.run(t, "trades")
	require.NoError(t, err)
	assert.NotContains(t, strings.ToLower(out), "slippage", "not requested is not unknown")

	out, err = e.run(t, "trades", "--format", "json")
	require.NoError(t, err)
	assert.NotContains(t, out, "execution_quality")
	assert.NotContains(t, out, "slippage")

	out, err = e.run(t, "trades", "--benchmark", "broker-vwap", "--format", "json")
	require.NoError(t, err)
	assert.Contains(t, out, "execution_quality")
}

func TestTrades_RangeSelectsByCloseTimeAfterReconstruction(t *testing.T) {
	e := newEnv(t).synced(t)
	// Trade A opened on day 0 and closed on day 2; the range holds only its
	// close, yet it is still a whole completed trade.
	out, err := e.run(t, "trades", "--from", "2024-03-02", "--to", "2024-03-05", "--format", "json")
	require.NoError(t, err)
	m := decode(t, out)
	trades := m["trades"].([]any)
	require.Len(t, trades, 1)
	assert.Equal(t, "2024-03-01T10:00:00Z", trades[0].(map[string]any)["opened_at"])
	assert.Empty(t, m["orphans"])

	out, err = e.run(t, "trades", "--from", "2030-01-01", "--format", "json")
	require.NoError(t, err)
	assert.Equal(t, []any{}, decode(t, out)["trades"])
}

func TestTrades_RequestedButUnmeasurableIsUnknown(t *testing.T) {
	e := newEnv(t).synced(t)
	// No fill reports a broker quote, so broker-quote cannot be built.
	out, err := e.run(t, "trades", "--benchmark", "broker-quote")
	require.NoError(t, err)
	assert.Contains(t, out, "SLIPPAGE_COST")
	assert.Contains(t, out, "unknown slippage cost:")
	assert.Contains(t, out, "no execution quote")

	// The account currency override is honored.
	out, err = e.run(t, "trades", "--benchmark", "broker-vwap", "--account-currency", "usd")
	require.NoError(t, err)
	assert.Contains(t, out, "USD (configured)")
	_, err = e.run(t, "trades", "--benchmark", "broker-vwap", "--account-currency", "dollars")
	assert.Error(t, err)
	_, err = e.run(t, "trades", "--benchmark", "mid")
	assert.ErrorContains(t, err, "--benchmark")
}

func firstTradeID(t *testing.T, e *env) string {
	out, err := e.run(t, "trades", "--format", "json")
	require.NoError(t, err)
	return decode(t, out)["trades"].([]any)[0].(map[string]any)["id"].(string)
}

func TestShow_FullIDPrefixAndForensics(t *testing.T) {
	e := newEnv(t).synced(t)
	id := firstTradeID(t, e)

	out, err := e.run(t, "trades", "show", id)
	require.NoError(t, err)
	golden(t, "show.golden", out)

	out, err = e.run(t, "trades", "show", id[:len(id)-2], "--format", "json")
	require.NoError(t, err)
	golden(t, "show.json.golden", out)
	m := decode(t, out)
	assert.Equal(t, id, m["trade"].(map[string]any)["id"], "a prefix resolves to the full ID")
	assert.Len(t, m["source_activities"], 2)

	out, err = e.run(t, "trades", "show", id, "--benchmark", "broker-vwap")
	require.NoError(t, err)
	golden(t, "show_benchmark.golden", out)
	assert.Contains(t, out, "broker_full_vwap")
	assert.Contains(t, out, "units-weighted")
	assert.Contains(t, out, "benchmark age")
}

func TestShow_AmbiguousAndMissing(t *testing.T) {
	e := newEnv(t).synced(t)
	_, err := e.run(t, "trades", "show", "oanda/practice/acct")
	require.Error(t, err)
	assert.ErrorContains(t, err, "matches 3 trades", "the completed trades and the open position")
	assert.ErrorContains(t, err, "oanda/practice/acct/2#long")
	assert.ErrorContains(t, err, "oanda/practice/acct/4#long")

	_, err = e.run(t, "trades", "show", "nothing")
	assert.ErrorContains(t, err, "no trade matches")
	_, err = e.run(t, "trades", "show")
	assert.Error(t, err)
	_, err = e.run(t, "trades", "show", "a", "b")
	assert.Error(t, err)

	out, err := e.run(t, "trades", "show", "oanda/practice/acct/6#long")
	require.NoError(t, err)
	assert.Contains(t, out, "status:")
	assert.Contains(t, out, "open")
}

// ---- stream identity and secrets ----

func TestStreamMustBeNamed(t *testing.T) {
	e := newEnv(t)
	_, err := e.runRaw(t, "activity", "list")
	assert.ErrorContains(t, err, "--environment is required")
	_, err = e.runRaw(t, "--environment", "practice", "activity", "list")
	assert.ErrorContains(t, err, "--account is required")
	_, err = e.runRaw(t, "--environment", "staging", "--account", "a", "activity", "list")
	assert.ErrorContains(t, err, "practice or live")
	_, err = e.runRaw(t, "--environment", "practice", "--account", "a", "--broker", "alpaca", "activity", "list")
	assert.ErrorContains(t, err, "unknown broker")
	_, err = e.run(t, "activity", "list", "--format", "xml")
	assert.ErrorContains(t, err, "--format")

	// The environment and account can come from the environment.
	e.d.environ = func() []string {
		return []string{"TRADER_ACCOUNTACTIVITY_ROOT=" + e.root, "TRADER_ACCOUNTACTIVITY_ENVIRONMENT=practice", "TRADER_ACCOUNTACTIVITY_ACCOUNT=acct"}
	}
	_, err = e.runRaw(t, "activity", "list")
	assert.NoError(t, err)

	// Environments are separate streams: a live read finds nothing.
	e.synced(t)
	out, err := e.runRaw(t, "--environment", "live", "--account", "acct", "activity", "list", "--format", "json")
	require.NoError(t, err)
	assert.Empty(t, decode(t, out)["activities"])
}

func TestNoSecretFlags(t *testing.T) {
	e := newEnv(t)
	for _, f := range []string{"--oanda-token", "--token", "--api-key", "--secret"} {
		_, err := e.run(t, "activity", "list", f, "x")
		assert.ErrorContains(t, err, "unknown flag", f)
	}
}

func TestToken_EnvOnlyAndNeverEchoed(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yml")
	require.NoError(t, os.WriteFile(cfgFile, []byte("oanda_token: from-the-file\n"), 0o600))
	_, err := accountactivitycfg.Load([]string{"TRADER_CONFIG=" + cfgFile}, nil)
	require.Error(t, err, "a secret in the config file is refused")
	assert.NotContains(t, err.Error(), "from-the-file")

	tok := filepath.Join(dir, "token")
	require.NoError(t, os.WriteFile(tok, []byte("s3cret-token\n"), 0o600))
	cfg, err := accountactivitycfg.Load([]string{"TRADER_OANDA_TOKEN_FILE=" + tok}, nil)
	require.NoError(t, err)
	assert.Equal(t, "s3cret-token", cfg.OANDAToken)

	cfg, err = accountactivitycfg.Load([]string{"TRADER_OANDA_TOKEN=from-env", "TRADER_OANDA_TOKEN_FILE=" + tok}, nil)
	require.NoError(t, err)
	assert.Equal(t, "from-env", cfg.OANDAToken, "the environment wins over the token file")

	// With the real provider factory and no token, the error says where to put
	// it and holds no secret.
	_, err = accountactivitycfg.NewSource(accountactivitycfg.Config{Broker: "oanda"}, stream, accountactivitycfg.Bounds{}, clock.NewSimulated(syncAt))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "TRADER_OANDA_TOKEN")
}

func TestDefaultLedgerRoot(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/xdg")
	root, err := accountactivitycfg.Config{}.Root()
	require.NoError(t, err)
	assert.Equal(t, "/xdg/trader/accountactivity", root)
	root, err = accountactivitycfg.Config{LedgerRoot: "/explicit"}.Root()
	require.NoError(t, err)
	assert.Equal(t, "/explicit", root)
}

// ---- JSON contract ----

func TestJSON_NumbersAreStringsAndSlicesAreNeverNull(t *testing.T) {
	e := newEnv(t).synced(t)
	for _, args := range [][]string{
		{"trades"}, {"trades", "--open"}, {"trades", "--benchmark", "broker-vwap"}, {"activity", "list"},
	} {
		out, err := e.run(t, append(args, "--format", "json")...)
		require.NoError(t, err)
		assert.NotContains(t, out, "null", args)
		assertNoJSONNumbers(t, args, decode(t, out))
		assert.EqualValues(t, 1, decode(t, out)["schema_version"], args)
	}
}

// assertNoJSONNumbers walks a decoded document: the only numbers allowed are
// counts and the schema version, never a financial figure.
func assertNoJSONNumbers(t *testing.T, args []string, v any) {
	allowed := map[string]bool{"schema_version": true, "matched": true, "limit": true, "benchmark_age_ms": true, "interval_ms": true}
	var walk func(key string, v any)
	walk = func(key string, v any) {
		switch x := v.(type) {
		case float64:
			assert.True(t, allowed[key], "%v: %q is a JSON number; financial figures must be decimal strings", args, key)
		case map[string]any:
			for k, c := range x {
				walk(k, c)
			}
		case []any:
			for _, c := range x {
				walk(key, c)
			}
		}
	}
	walk("", v)
}

func TestJSON_UnknownAndKnownZeroAreDistinct(t *testing.T) {
	e := newEnv(t).synced(t)
	out, err := e.run(t, "trades", "--format", "json")
	require.NoError(t, err)
	m := decode(t, out)
	a := m["trades"].([]any)[0].(map[string]any)
	fin := a["financing"].(map[string]any)
	assert.Equal(t, "known", fin["state"])
	assert.Equal(t, "0.00", fin["value"], "a reported zero is a known zero")
	spread := a["spread_cost"].(map[string]any)
	assert.Equal(t, "unknown", spread["state"])
	assert.NotContains(t, spread, "value")
	assert.NotContains(t, spread, "currency")
	assert.Equal(t, "derived", fin["basis"], "a trade total is derived from the reported fills")
	assert.Equal(t, "unknown", a["net"].(map[string]any)["state"], "the entry fill supplied no commission")

	b := m["trades"].([]any)[1].(map[string]any)
	assert.Equal(t, "0.30", b["spread_cost"].(map[string]any)["value"])
	net := b["net"].(map[string]any)
	assert.Equal(t, "known", net["state"])
	assert.Equal(t, "derived", net["basis"])
}

// The stream identity must not share environment variables with the simulated
// broker commands: TRADER_ACCOUNT_ID names a simulator account there, and
// reading it here as a real broker account would be a quiet disaster.
func TestSimulatedBrokerAccountIDIsNotTheBrokerAccount(t *testing.T) {
	e := newEnv(t)
	e.d.environ = func() []string {
		return []string{"TRADER_ACCOUNTACTIVITY_ROOT=" + e.root, "TRADER_ACCOUNT_ID=sim-account", "TRADER_CURRENCY=EUR", "TRADER_PROVIDER=alpaca"}
	}
	_, err := e.runRaw(t, "--environment", "practice", "activity", "list")
	assert.ErrorContains(t, err, "--account is required")
}

// ---- audit fidelity ----

func eurMoney(s string) accountactivity.Value[num.Money] {
	return accountactivity.Reported(num.MustParseMoney(s, usd))
}

// feeOnly is a valid fee activity that carries a commission and no other fee.
func feeOnly() accountactivity.Activity {
	return accountactivity.Activity{
		Provider: stream.Provider, Environment: stream.Environment, ProviderAccountID: stream.ProviderAccountID,
		ProviderActivityID: "20", ProviderType: "TRANSFER_FUNDS", Kind: accountactivity.KindFee,
		OccurredAt: t0.Add(time.Hour), Commission: eurMoney("-2.5"),
	}
}

func TestFeeCommissionIsNotDropped(t *testing.T) {
	e := newEnvWith(t, []accountactivity.Activity{deposit("1", 0, "100"), feeOnly()}).synced(t)

	out, err := e.run(t, "activity", "list", "--kind", "fee")
	require.NoError(t, err)
	assert.Contains(t, out, "commission=-2.50 USD")
	assert.Contains(t, out, "other_fees=unknown", "the absent component is still shown, as unknown")

	out, err = e.run(t, "activity", "list", "--kind", "fee", "--format", "json")
	require.NoError(t, err)
	fee := decode(t, out)["activities"].([]any)[0].(map[string]any)
	commission := fee["commission"].(map[string]any)
	assert.Equal(t, "known", commission["state"])
	assert.Equal(t, "-2.50", commission["value"])
	assert.Equal(t, "reported", commission["basis"])
	assert.Equal(t, "unknown", fee["other_fees"].(map[string]any)["state"])
}

// reversalFill sells 150 while 100 is long: it closes trade t1 and opens a
// short t2, carrying per-trade amounts and the broker's whole quote snapshot.
func reversalFixture() []accountactivity.Activity {
	open := withEffect(fill(fillSpec{id: "10", at: 0, side: order.Buy, qty: "100", price: "1.1000", vwap: "1.1000"}), accountactivity.TradeRoleOpened, "100", "1.1000")
	rev := fill(fillSpec{id: "11", at: time.Hour + 250*time.Millisecond, side: order.Sell, qty: "150", price: "1.1100", vwap: "1.1101"})
	rev.TradeEffects = []accountactivity.TradeEffect{
		{BrokerTradeID: "t1", Role: accountactivity.TradeRoleClosed,
			Units: accountactivity.Reported(num.MustParseQuantity("100")), Price: accountactivity.Reported(num.MustParsePrice("1.1100")),
			RealizedPL: eurMoney("10"), Financing: eurMoney("-0.4"), SpreadCost: eurMoney("0.2"), GuaranteedExecutionFee: eurMoney("-0.1")},
		{BrokerTradeID: "t2", Role: accountactivity.TradeRoleOpened,
			Units: accountactivity.Reported(num.MustParseQuantity("50")), Price: accountactivity.Reported(num.MustParsePrice("1.1100")),
			RealizedPL: eurMoney("0"), Financing: eurMoney("0"), SpreadCost: eurMoney("0.1"), GuaranteedExecutionFee: eurMoney("0")},
	}
	rev.ExecutionQuote = accountactivity.Reported(accountactivity.ExecutionQuote{
		At:          t0.Add(time.Hour + 249*time.Millisecond + 500*time.Microsecond),
		Bids:        []accountactivity.PriceLevel{{Price: num.MustParsePrice("1.1099"), Liquidity: num.MustParseQuantity("100")}, {Price: num.MustParsePrice("1.1098"), Liquidity: num.MustParseQuantity("500")}},
		Asks:        []accountactivity.PriceLevel{{Price: num.MustParsePrice("1.1101"), Liquidity: num.MustParseQuantity("100")}},
		CloseoutBid: accountactivity.Reported(num.MustParsePrice("1.1090")), CloseoutAsk: accountactivity.Reported(num.MustParsePrice("1.1110")),
	})
	rev.Commission, rev.OtherFees, rev.RealizedPL, rev.Financing, rev.SpreadCost = eurMoney("-0.6"), eurMoney("0"), eurMoney("10"), eurMoney("-0.4"), eurMoney("0.3")
	return []accountactivity.Activity{open, rev}
}

func TestShowKeepsEffectAmountsAndTheWholeQuote(t *testing.T) {
	e := newEnvWith(t, reversalFixture()).synced(t)
	out, err := e.run(t, "trades", "show", "oanda/practice/acct/10#long", "--format", "json")
	require.NoError(t, err)
	golden(t, "show_reversal.json.golden", out)

	m := decode(t, out)
	fills := m["source_activities"].([]any)
	require.Len(t, fills, 2)
	rev := fills[1].(map[string]any)

	effects := rev["trade_effects"].([]any)
	require.Len(t, effects, 2)
	t1 := effects[0].(map[string]any)
	assert.Equal(t, "t1", t1["broker_trade_id"])
	for field, want := range map[string]string{"realized_pl": "10.00", "financing": "-0.40", "spread_cost": "0.20", "guaranteed_execution_fee": "-0.10"} {
		v := t1[field].(map[string]any)
		assert.Equal(t, "known", v["state"], field)
		assert.Equal(t, want, v["value"], field)
		assert.Equal(t, "reported", v["basis"], field)
	}
	assert.Equal(t, "0.00", effects[1].(map[string]any)["realized_pl"].(map[string]any)["value"], "a reported zero is a known zero")

	q := rev["execution_quote"].(map[string]any)
	assert.Equal(t, "reported", q["basis"])
	assert.Equal(t, "2024-03-01T10:00:00.2495Z", q["at"], "the quote's sub-second instant is kept")
	assert.Len(t, q["bids"], 2, "the whole ladder, not the touch")
	assert.Equal(t, map[string]any{"price": "1.1098", "liquidity": "500"}, q["bids"].([]any)[1])
	assert.Equal(t, "1.111", q["closeout_ask"].(map[string]any)["value"])
	assert.Equal(t, "1.109", q["closeout_bid"].(map[string]any)["value"])

	tab, err := e.run(t, "trades", "show", "oanda/practice/acct/10#long")
	require.NoError(t, err)
	golden(t, "show_reversal.golden", tab)
	for _, want := range []string{
		"trade effect:", "t1 (closed)", "t2 (opened)", "guaranteed exec fee", "-0.10 USD [reported]",
		"execution quote:", "quoted at", "2024-03-01T10:00:00.2495Z", "1.1099@100 1.1098@500", "1.1101@100", "closeout bid", "closeout ask",
	} {
		assert.Contains(t, tab, want)
	}
}

func TestTimestampsKeepSubsecondPrecision(t *testing.T) {
	a := deposit("1", 0, "100")
	a.OccurredAt = t0.Add(123456789 * time.Nanosecond)
	b := deposit("2", 0, "100")
	b.OccurredAt = t0.Add(123456790 * time.Nanosecond)
	e := newEnvWith(t, []accountactivity.Activity{a, b}).synced(t)

	out, err := e.run(t, "activity", "list", "--format", "json")
	require.NoError(t, err)
	acts := decode(t, out)["activities"].([]any)
	assert.Equal(t, "2024-03-01T09:00:00.123456789Z", acts[0].(map[string]any)["occurred_at"])
	assert.Equal(t, "2024-03-01T09:00:00.12345679Z", acts[1].(map[string]any)["occurred_at"], "two fills in one second stay distinguishable")

	tab, err := e.run(t, "activity", "list")
	require.NoError(t, err)
	assert.Contains(t, tab, "2024-03-01T09:00:00.123456789Z")

	// The range echoed back is the instant the query used.
	out, err = e.run(t, "activity", "list", "--from", "2024-03-01T09:00:00.123456790Z", "--format", "json")
	require.NoError(t, err)
	m := decode(t, out)
	assert.Equal(t, "2024-03-01T09:00:00.12345679Z", m["range"].(map[string]any)["from"])
	assert.EqualValues(t, 1, m["matched"], "the range excludes the earlier nanosecond")

	// Whole seconds stay short.
	assert.Equal(t, "2024-03-01T09:00:00Z", timeText(t0))
}

func TestOpenCannotBeCombinedWithBenchmark(t *testing.T) {
	e := newEnv(t).synced(t)
	for _, b := range []string{"broker-vwap", "broker-quote"} {
		_, err := e.run(t, "trades", "--open", "--benchmark", b)
		require.Error(t, err, "a requested benchmark must not be silently ignored")
		assert.ErrorContains(t, err, "--benchmark does not apply to --open")
		assert.ErrorContains(t, err, "trades show")
	}
	// Each flag alone still works; an open position's fills are measured via show.
	_, err := e.run(t, "trades", "--open")
	assert.NoError(t, err)
	out, err := e.run(t, "trades", "show", "oanda/practice/acct/6#long", "--benchmark", "broker-vwap")
	require.NoError(t, err)
	assert.Contains(t, out, "execution quality")
}
