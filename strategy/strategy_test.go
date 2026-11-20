package strategy

import (
	"context"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func p(s string) num.Price { return num.MustParsePrice(s) }

var frame = time.Date(2024, 3, 1, 11, 0, 0, 0, time.UTC)

func longOpen() Order {
	return Order{Instrument: "EURUSD", Action: Open, Direction: Long, Type: Market,
		Price: p("1.08764"), StopLoss: p("1.08500"), TakeProfit: p("1.09200"), Trail: p("0.002"), Ref: "emax-1"}
}

func TestOrder_Validate_LegalRequests(t *testing.T) {
	short := Order{Instrument: "GBPUSD", Action: Open, Direction: Short, Type: Limit, Price: p("1.26600"),
		StopLoss: p("1.26900"), TakeProfit: p("1.26000"), Expires: frame.Add(9 * time.Hour)}
	for name, o := range map[string]Order{
		"long market open":          longOpen(),
		"default type is market":    {Instrument: "EURUSD", Action: Open, Direction: Long, Price: p("1.1"), StopLoss: p("1.09")},
		"short limit open":          short,
		"close":                     {Instrument: "EURUSD", Action: Close, Ref: "x"},
		"reduce":                    {Instrument: "EURUSD", Action: Reduce},
		"add":                       {Instrument: "EURUSD", Action: Add},
		"amend a stop":              {Instrument: "EURUSD", Action: Amend, StopLoss: p("1.095")},
		"amend a target":            {Instrument: "EURUSD", Action: Amend, TakeProfit: p("1.12")},
		"amend a trail":             {Instrument: "EURUSD", Action: Amend, Trail: p("0.0015")},
		"amend all three":           {Instrument: "EURUSD", Action: Amend, StopLoss: p("1.095"), TakeProfit: p("1.12"), Trail: p("0.0015")},
		"stop order entry":          {Instrument: "EURUSD", Action: Open, Direction: Long, Type: Stop, Price: p("1.105"), StopLoss: p("1.094")},
		"maximum price":             {Instrument: "EURUSD", Action: Open, Direction: Long, Price: p("10000"), StopLoss: p("9999")},
		"ref of the maximum length": {Instrument: "EURUSD", Action: Close, Ref: strings.Repeat("a", MaxRefLen)},
	} {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, o.Validate(frame))
		})
	}
}

func TestOrder_Validate_Refusals(t *testing.T) {
	with := func(f func(*Order)) Order { o := longOpen(); f(&o); return o }
	for name, tc := range map[string]struct {
		o      Order
		member string
	}{
		"no instrument":                  {with(func(o *Order) { o.Instrument = "" }), "instrument"},
		"unknown action":                 {with(func(o *Order) { o.Action = "buy" }), "action"},
		"action in another case":         {with(func(o *Order) { o.Action = "Open" }), "action"},
		"unknown type":                   {with(func(o *Order) { o.Type = "market-if-touched" }), "type"},
		"limit that is not an open":      {Order{Instrument: "EURUSD", Action: Close, Type: Limit}, "type"},
		"open without a direction":       {with(func(o *Order) { o.Direction = "" }), "direction"},
		"open with an unknown direction": {with(func(o *Order) { o.Direction = "up" }), "direction"},
		"open without a price":           {with(func(o *Order) { o.Price = num.Price{} }), "price"},
		"open without a stop":            {with(func(o *Order) { o.StopLoss = num.Price{} }), "stop_loss"},
		"long stop at the price":         {with(func(o *Order) { o.StopLoss = o.Price }), "stop_loss"},
		"long stop above the price":      {with(func(o *Order) { o.StopLoss = p("1.09") }), "stop_loss"},
		"short stop below the price":     {with(func(o *Order) { o.Direction, o.StopLoss, o.TakeProfit = Short, p("1.08"), p("1.07") }), "stop_loss"},
		"long target at the price":       {with(func(o *Order) { o.TakeProfit = o.Price }), "take_profit"},
		"short target above the price":   {with(func(o *Order) { o.Direction, o.StopLoss, o.TakeProfit = Short, p("1.09"), p("1.095") }), "take_profit"},
		"expiry at the frame":            {with(func(o *Order) { o.Expires = frame }), "expires"},
		"expiry before the frame":        {with(func(o *Order) { o.Expires = frame.Add(-time.Hour) }), "expires"},
		"price above the maximum":        {with(func(o *Order) { o.Price, o.StopLoss, o.TakeProfit = p("10000.00001"), p("1"), num.Price{} }), "price"},
		"close with a direction":         {Order{Instrument: "EURUSD", Action: Close, Direction: Long}, "direction"},
		"reduce with a price":            {Order{Instrument: "EURUSD", Action: Reduce, Price: p("1.1")}, "price"},
		"close with a stop":              {Order{Instrument: "EURUSD", Action: Close, StopLoss: p("1.09")}, "stop_loss"},
		"close with a target":            {Order{Instrument: "EURUSD", Action: Close, TakeProfit: p("1.12")}, "take_profit"},
		"add with a trail":               {Order{Instrument: "EURUSD", Action: Add, Trail: p("0.002")}, "trail"},
		"close with an expiry":           {Order{Instrument: "EURUSD", Action: Close, Expires: frame.Add(time.Hour)}, "expires"},
		"amend with nothing to change":   {Order{Instrument: "EURUSD", Action: Amend}, "action"},
		"amend with a direction":         {Order{Instrument: "EURUSD", Action: Amend, Direction: Long, StopLoss: p("1.09")}, "direction"},
		"amend with a price":             {Order{Instrument: "EURUSD", Action: Amend, Price: p("1.1"), StopLoss: p("1.09")}, "price"},
		"amend with an expiry":           {Order{Instrument: "EURUSD", Action: Amend, StopLoss: p("1.09"), Expires: frame.Add(time.Hour)}, "expires"},
		"amend that is a limit order":    {Order{Instrument: "EURUSD", Action: Amend, Type: Limit, StopLoss: p("1.09")}, "type"},
		"amend target above the maximum": {Order{Instrument: "EURUSD", Action: Amend, TakeProfit: p("10001")}, "take_profit"},
		"ref too long":                   {with(func(o *Order) { o.Ref = strings.Repeat("a", MaxRefLen+1) }), "ref"},
		"ref with a control character":   {with(func(o *Order) { o.Ref = "a\nb" }), "ref"},
		"ref that is not ASCII":          {with(func(o *Order) { o.Ref = "café" }), "ref"},
	} {
		t.Run(name, func(t *testing.T) {
			err := tc.o.Validate(frame)
			require.ErrorIs(t, err, ErrInvalidOrder)
			assert.Contains(t, err.Error(), tc.member+":", "the error names the member")
		})
	}
}

func TestOrder_Validate_ZeroFrameSkipsTheExpiryCheck(t *testing.T) {
	o := longOpen()
	o.Expires = frame.Add(-time.Hour)

	require.NoError(t, o.Validate(time.Time{}))
	require.Error(t, o.Validate(frame))
}

func TestOrder_Validate_ASpacePrintableRefIsLegal(t *testing.T) {
	o := Order{Instrument: "EURUSD", Action: Close, Ref: " ~ "}

	require.NoError(t, o.Validate(frame))
}

// sample proves the two interfaces are what a strategy in another repository
// would write.
type sample struct{ warmed int }

func (s *sample) OnBars(context.Context, []marketdata.Bar) ([]Order, error) { return nil, nil }
func (s *sample) Warm(_ context.Context, bars []marketdata.Bar)             { s.warmed += len(bars) }

var (
	_ Strategy = (*sample)(nil)
	_ Warmer   = (*sample)(nil)
)

func TestStrategy_ASampleSatisfiesBothInterfaces(t *testing.T) {
	s := &sample{}

	s.Warm(context.Background(), make([]marketdata.Bar, 3))
	orders, err := s.OnBars(context.Background(), nil)

	require.NoError(t, err)
	assert.Empty(t, orders)
	assert.Equal(t, 3, s.warmed)
}

// TestStrategy_ImportsOnlyThePublicModel keeps the seam importable from another
// repository: within the module it may import only the public data model.
func TestStrategy_ImportsOnlyThePublicModel(t *testing.T) {
	const mod = "github.com/rustyeddy/trader"
	allowed := map[string]bool{mod + "/marketdata": true, mod + "/num": true, mod + "/instrument": true}
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	checked := 0
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		checked++
		f, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
		require.NoError(t, err)
		for _, imp := range f.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			require.NoError(t, err)
			if path == mod || strings.HasPrefix(path, mod+"/") {
				require.True(t, allowed[path], "%s imports %s", file, path)
			}
		}
	}
	require.Positive(t, checked)
}

func TestAction_ValuesAreTheWireStrings(t *testing.T) {
	assert.Equal(t, []string{"open", "add", "reduce", "close", "amend"},
		[]string{string(Open), string(Add), string(Reduce), string(Close), string(Amend)})
	assert.Equal(t, []string{"long", "short", "market", "limit", "stop"},
		[]string{string(Long), string(Short), string(Market), string(Limit), string(Stop)})
}
