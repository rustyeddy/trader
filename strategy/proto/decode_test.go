package proto

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/strategy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	hdr = `"v":1,"type":"%s","seq":%d,"run_id":"r-1"`
	reg = `{"v":1,"type":"register","seq":1,"name":"emax","version":"0.3","build":"git:9f2c1ab","config":{"short":12},"capabilities":["bars","warm"],` +
		`"run":{"mode":"backtest","interval":"H1","instruments":["EURUSD","GBPUSD"],"from":"2024-03-01T00:00:00Z","to":"2024-04-01T00:00:00Z","warm_bars":100}}`
)

func ordersWith(members string) string {
	return fmt.Sprintf(`{`+hdr+`,"in_reply_to":2,"orders":[{%s}]}`, "orders", 3, members)
}

const goodOpen = `"instrument":"EURUSD","action":"open","direction":"long","type":"market","price":"1.08764","stop_loss":"1.085","take_profit":"1.092","trail":"0.002","ref":"emax-1"`

func decode(t *testing.T, s string) Message {
	t.Helper()
	m, err := Decode([]byte(s))
	require.NoError(t, err)
	return m
}

func TestDecode_Register(t *testing.T) {
	m := decode(t, reg).(*Register)

	assert.Equal(t, Header{V: 1, Type: TypeRegister, Seq: 1}, m.Header)
	assert.Equal(t, "emax", m.Name)
	assert.Equal(t, "0.3", m.Version)
	assert.Equal(t, "git:9f2c1ab", m.Build)
	assert.JSONEq(t, `{"short":12}`, string(m.Config))
	assert.Equal(t, []string{"bars", "warm"}, m.Capabilities)
	assert.Equal(t, Run{Mode: "backtest", Interval: "H1", Instruments: []string{"EURUSD", "GBPUSD"},
		From: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC), WarmBars: 100}, m.Run)
}

func TestDecode_RegisterIgnoresUnknownMembersOutsideRun(t *testing.T) {
	in := strings.Replace(reg, `"name":"emax",`, `"name":"emax","client_note":"hi","extra":{"a":[1]},`, 1)

	m := decode(t, in).(*Register)

	assert.Equal(t, "emax", m.Name)
}

func TestDecode_RegisterOptionalMembers(t *testing.T) {
	in := `{"v":1,"type":"register","seq":1,"name":"x","capabilities":["bars"],"run":{"mode":"paper","interval":"D1","instruments":["EURUSD"],"from":"2024-03-01T00:00:00Z"}}`

	m := decode(t, in).(*Register)

	assert.Empty(t, m.Version)
	assert.Empty(t, m.Build)
	assert.Nil(t, m.Config)
	assert.True(t, m.Run.To.IsZero())
	assert.Zero(t, m.Run.WarmBars)
}

func TestDecode_RunIsStrict(t *testing.T) {
	for name, in := range map[string]string{
		"unknown member":       strings.Replace(reg, `"warm_bars":100`, `"warm_bars":100,"instrument":"EURUSD"`, 1),
		"a typo":               strings.Replace(reg, `"instruments"`, `"instrument"`, 1),
		"null member":          strings.Replace(reg, `"warm_bars":100`, `"warm_bars":null`, 1),
		"missing mode":         strings.Replace(reg, `"mode":"backtest",`, ``, 1),
		"missing instruments":  strings.Replace(reg, `"instruments":["EURUSD","GBPUSD"],`, ``, 1),
		"missing from":         strings.Replace(reg, `"from":"2024-03-01T00:00:00Z",`, ``, 1),
		"bad from":             strings.Replace(reg, `2024-03-01T00:00:00Z`, `2024-03-01`, 1),
		"fractional warm":      strings.Replace(reg, `"warm_bars":100`, `"warm_bars":1.5`, 1),
		"instrument as number": strings.Replace(reg, `["EURUSD","GBPUSD"]`, `[1]`, 1),
		"run as array":         strings.Replace(reg, `"run":{`, `"run":[{`, 1)[:0] + `{"v":1,"type":"register","seq":1,"name":"x","capabilities":["bars"],"run":[]}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Decode([]byte(in))

			require.Error(t, err)
			assert.Equal(t, CodeRejected, CodeOf(err), "%v", err)
		})
	}
}

func TestDecode_RunErrorNamesTheMember(t *testing.T) {
	_, err := Decode([]byte(strings.Replace(reg, `"warm_bars":100`, `"warm_bars":100,"instrument":"EURUSD"`, 1)))

	assert.Contains(t, err.Error(), `"instrument"`)
}

func TestDecode_RegisterRefusals(t *testing.T) {
	for name, in := range map[string]string{
		"no name":                  strings.Replace(reg, `"name":"emax",`, ``, 1),
		"name as number":           strings.Replace(reg, `"name":"emax"`, `"name":7`, 1),
		"config as an array":       strings.Replace(reg, `"config":{"short":12}`, `"config":[1]`, 1),
		"config null":              strings.Replace(reg, `"config":{"short":12}`, `"config":null`, 1),
		"no capabilities":          strings.Replace(reg, `"capabilities":["bars","warm"],`, ``, 1),
		"capabilities not strings": strings.Replace(reg, `["bars","warm"]`, `[1]`, 1),
		"no run":                   `{"v":1,"type":"register","seq":1,"name":"x","capabilities":["bars"]}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Decode([]byte(in))

			require.Error(t, err)
			assert.Equal(t, CodeProtocol, CodeOf(err), "%v", err)
		})
	}
}

func TestDecode_Accepted(t *testing.T) {
	in := `{"v":1,"type":"accepted","seq":1,"run_id":"r-20241103-7f3a9c","instruments":["EURUSD","GBPUSD"],"interval":"H1","warm_bars":100,"reply_timeout_ms":30000}`

	m := decode(t, in).(*Accepted)

	assert.Equal(t, Header{V: 1, Type: TypeAccepted, Seq: 1, RunID: "r-20241103-7f3a9c"}, m.Header)
	assert.Equal(t, []string{"EURUSD", "GBPUSD"}, m.Instruments)
	assert.Equal(t, "H1", m.Interval)
	assert.Equal(t, 100, m.WarmBars)
	assert.Equal(t, 30000, m.ReplyTimeoutMS)
}

func TestDecode_AcceptedRefusals(t *testing.T) {
	good := `{"v":1,"type":"accepted","seq":1,"run_id":"r","instruments":["EURUSD"],"interval":"H1","reply_timeout_ms":30000}`
	require.NotNil(t, decode(t, good))
	for name, in := range map[string]string{
		"no instruments": strings.Replace(good, `"instruments":["EURUSD"],`, ``, 1),
		"no interval":    strings.Replace(good, `"interval":"H1",`, ``, 1),
		"no timeout":     strings.Replace(good, `,"reply_timeout_ms":30000`, ``, 1),
		"negative warm":  strings.Replace(good, `"interval"`, `"warm_bars":-1,"interval"`, 1),
	} {
		_, err := Decode([]byte(in))
		require.Error(t, err, name)
		assert.Equal(t, CodeProtocol, CodeOf(err), name)
	}
}

const goodBars = `{"v":1,"type":"bars","seq":2,"run_id":"r-1","frame_at":"2024-03-01T11:00:00Z","warm":false,"bars":[` +
	`{"instrument":"EURUSD","time":"2024-03-01T10:00:00Z","available_at":"2024-03-01T11:00:00Z",` +
	`"bid":{"open":"1.08725","high":"1.0879","low":"1.0869","close":"1.0876"},` +
	`"ask":{"open":"1.08728","high":"1.08794","low":"1.08693","close":"1.08764"},` +
	`"mid":{"open":"1.087265","high":"1.08792","low":"1.086915","close":"1.08762"},"volume":2310}]}`

func TestDecode_Bars(t *testing.T) {
	m := decode(t, goodBars).(*Bars)

	assert.Equal(t, time.Date(2024, 3, 1, 11, 0, 0, 0, time.UTC), m.FrameAt)
	assert.False(t, m.Warm)
	require.Len(t, m.Bars, 1)
	b := m.Bars[0]
	assert.Equal(t, "EURUSD", b.Instrument)
	assert.Equal(t, time.Date(2024, 3, 1, 10, 0, 0, 0, time.UTC), b.Time)
	assert.Equal(t, time.Date(2024, 3, 1, 11, 0, 0, 0, time.UTC), b.AvailableAt)
	assert.Equal(t, int64(2310), b.Volume)
	assert.Equal(t, "1.087265", b.Mid.Open.String(), "a six-decimal mid keeps every digit")
	assert.Equal(t, "1.0879", b.Bid.High.String())
	assert.True(t, b.Trade.IsZero(), "a series the bar lacks stays absent")
}

func TestDecode_BarsWarmFlagAndUnknownMembers(t *testing.T) {
	in := strings.Replace(goodBars, `"warm":false`, `"warm":true,"future":1`, 1)
	in = strings.Replace(in, `"volume":2310`, `"volume":2310,"spread":"0.00003"`, 1)

	m := decode(t, in).(*Bars)

	assert.True(t, m.Warm)
}

func TestDecode_BarsRefusals(t *testing.T) {
	for name, in := range map[string]string{
		"no frame_at":          strings.Replace(goodBars, `"frame_at":"2024-03-01T11:00:00Z",`, ``, 1),
		"empty bars":           strings.Replace(goodBars, goodBars[strings.Index(goodBars, `"bars":[`):], `"bars":[]}`, 1),
		"no bars":              strings.Replace(goodBars, goodBars[strings.Index(goodBars, `,"bars":[`):], `}`, 1),
		"warm as string":       strings.Replace(goodBars, `"warm":false`, `"warm":"no"`, 1),
		"price as a number":    strings.Replace(goodBars, `"open":"1.08725"`, `"open":1.08725`, 1),
		"price with exponent":  strings.Replace(goodBars, `"open":"1.08725"`, `"open":"1.08725e0"`, 1),
		"price missing":        strings.Replace(goodBars, `"open":"1.08725",`, ``, 1),
		"no instrument":        strings.Replace(goodBars, `"instrument":"EURUSD",`, ``, 1),
		"bad time":             strings.Replace(goodBars, `"time":"2024-03-01T10:00:00Z"`, `"time":"2024-03-01T10:00:00+00:00"`, 1),
		"no available_at":      strings.Replace(goodBars, `"available_at":"2024-03-01T11:00:00Z",`, ``, 1),
		"fractional volume":    strings.Replace(goodBars, `"volume":2310`, `"volume":2310.5`, 1),
		"series not an object": strings.Replace(goodBars, `"bid":{"open":"1.08725","high":"1.0879","low":"1.0869","close":"1.0876"}`, `"bid":"1.08"`, 1),
		"bar not an object":    strings.Replace(goodBars, `"bars":[{`, `"bars":[1,{`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Decode([]byte(in))

			require.Error(t, err)
			assert.Equal(t, CodeProtocol, CodeOf(err), "%v", err)
		})
	}
}

func TestDecode_Orders(t *testing.T) {
	in := `{"v":1,"type":"orders","seq":2,"run_id":"r-1","in_reply_to":2,"orders":[` +
		`{` + goodOpen + `},` +
		`{"instrument":"GBPUSD","action":"open","direction":"short","type":"limit","price":"1.266","stop_loss":"1.269","take_profit":"1.26","expires":"2024-03-01T20:00:00Z"},` +
		`{"instrument":"GBPUSD","action":"close","ref":"x"},` +
		`{"instrument":"EURUSD","action":"amend","stop_loss":"1.0876","trail":"0.0015","take_profit":"1.1"}]}`

	m := decode(t, in).(*Orders)

	assert.Equal(t, uint64(2), m.InReplyTo)
	require.Len(t, m.Orders, 4)
	assert.Equal(t, strategy.Order{Instrument: "EURUSD", Action: strategy.Open, Direction: strategy.Long, Type: strategy.Market,
		Price: num.MustParsePrice("1.08764"), StopLoss: num.MustParsePrice("1.085"), TakeProfit: num.MustParsePrice("1.092"),
		Trail: num.MustParsePrice("0.002"), Ref: "emax-1"}, m.Orders[0])
	assert.Equal(t, strategy.Limit, m.Orders[1].Type)
	assert.Equal(t, time.Date(2024, 3, 1, 20, 0, 0, 0, time.UTC), m.Orders[1].Expires)
	assert.Equal(t, strategy.Order{Instrument: "GBPUSD", Action: strategy.Close, Type: strategy.Market, Ref: "x"}, m.Orders[2], "no type is market")
	assert.Equal(t, strategy.Amend, m.Orders[3].Action)
	assert.Equal(t, "0.0015", m.Orders[3].Trail.String())
}

func TestDecode_AnEmptyOrdersReplyIsLegal(t *testing.T) {
	m := decode(t, `{"v":1,"type":"orders","seq":2,"run_id":"r-1","in_reply_to":2,"orders":[]}`).(*Orders)

	assert.Empty(t, m.Orders)
}

func TestDecode_UnknownMembersOutsideTheOrderAreTolerated(t *testing.T) {
	m := decode(t, `{"v":1,"type":"orders","seq":4,"run_id":"r-1","in_reply_to":4,"client_note":"ignored","orders":[]}`).(*Orders)

	assert.Empty(t, m.Orders)
}

// The negative conformance cases N14 to N19 of ADR-094: an order object that
// carries a member Trader owns, an unknown member, or a value out of form.
func TestDecode_OrderObjectsAreStrict(t *testing.T) {
	for name, members := range map[string]string{
		"N14 units":                   goodOpen + `,"units":1000`,
		"N14 units as a string":       goodOpen + `,"units":"1000"`,
		"N15 accountID":               goodOpen + `,"accountID":"a"`,
		"N15 account_id":              goodOpen + `,"account_id":"a"`,
		"N15 AccountID":               goodOpen + `,"AccountID":"a"`,
		"N16 id":                      goodOpen + `,"id":"t-1"`,
		"N16 state":                   goodOpen + `,"state":"filled"`,
		"N16 fill_price":              goodOpen + `,"fill_price":"1.1"`,
		"N16 fillPrice":               goodOpen + `,"fillPrice":"1.1"`,
		"N16 reason":                  goodOpen + `,"reason":"x"`,
		"N16 decided_at":              goodOpen + `,"decided_at":"2024-03-01T11:00:00Z"`,
		"N16 history":                 goodOpen + `,"history":[]`,
		"N16 host-owned with a null":  goodOpen + `,"units":null`,
		"N16a stoploss":               goodOpen + `,"stoploss":"1.08"`,
		"N16a takeprofit typo":        strings.Replace(goodOpen, `"take_profit"`, `"take_profitt"`, 1),
		"N16a Stop_Loss":              goodOpen + `,"Stop_Loss":"1.08"`,
		"N16a a note":                 goodOpen + `,"note":"hello"`,
		"N16b null member":            strings.Replace(goodOpen, `"trail":"0.002"`, `"trail":null`, 1),
		"N16b null instrument":        strings.Replace(goodOpen, `"instrument":"EURUSD"`, `"instrument":null`, 1),
		"N16b unknown action":         strings.Replace(goodOpen, `"action":"open"`, `"action":"buy"`, 1),
		"N16b action in another case": strings.Replace(goodOpen, `"action":"open"`, `"action":"Open"`, 1),
		"N16b unknown direction":      strings.Replace(goodOpen, `"direction":"long"`, `"direction":"LONG"`, 1),
		"N16b unknown type":           strings.Replace(goodOpen, `"type":"market"`, `"type":"mkt"`, 1),
		"N16c empty ref":              strings.Replace(goodOpen, `"ref":"emax-1"`, `"ref":""`, 1),
		"N16c ref as a number":        strings.Replace(goodOpen, `"ref":"emax-1"`, `"ref":7`, 1),
		"N19 price as a number":       strings.Replace(goodOpen, `"price":"1.08764"`, `"price":1.08764`, 1),
		"N19 stop as a number":        strings.Replace(goodOpen, `"stop_loss":"1.085"`, `"stop_loss":1.085`, 1),
		"price with an exponent":      strings.Replace(goodOpen, `"price":"1.08764"`, `"price":"1.08764e0"`, 1),
		"price with nine decimals":    strings.Replace(goodOpen, `"price":"1.08764"`, `"price":"1.087640001"`, 1),
		"price negative":              strings.Replace(goodOpen, `"price":"1.08764"`, `"price":"-1.08764"`, 1),
		"N13 zero price":              strings.Replace(goodOpen, `"price":"1.08764"`, `"price":"0"`, 1),
		"N6c zero stop":               strings.Replace(goodOpen, `"stop_loss":"1.085"`, `"stop_loss":"0"`, 1),
		"N6c zero trail":              strings.Replace(goodOpen, `"trail":"0.002"`, `"trail":"0.00000"`, 1),
		"bad expiry":                  goodOpen + `,"expires":"2024-03-01"`,
		"no instrument":               strings.Replace(goodOpen, `"instrument":"EURUSD",`, ``, 1),
		"no action":                   strings.Replace(goodOpen, `"action":"open",`, ``, 1),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Decode([]byte(ordersWith(members)))

			require.Error(t, err)
			assert.Equal(t, CodeInvalidOrder, CodeOf(err), "%v", err)
		})
	}
}

func TestDecode_AHostOwnedMemberIsNamedWhateverItsValue(t *testing.T) {
	_, err := Decode([]byte(ordersWith(goodOpen + `,"units":0`)))

	assert.Contains(t, err.Error(), `"units"`)
	assert.Contains(t, err.Error(), "orders[0]")
}

func TestDecode_AnUnknownOrderMemberIsNamed(t *testing.T) {
	_, err := Decode([]byte(ordersWith(goodOpen + `,"stoploss":"1.08"`)))

	assert.Contains(t, err.Error(), `"stoploss"`)
}

func TestDecode_OrdersStructuralRefusals(t *testing.T) {
	for name, in := range map[string]string{
		"no in_reply_to":    `{"v":1,"type":"orders","seq":2,"run_id":"r","orders":[]}`,
		"no orders":         `{"v":1,"type":"orders","seq":2,"run_id":"r","in_reply_to":2}`,
		"orders as object":  `{"v":1,"type":"orders","seq":2,"run_id":"r","in_reply_to":2,"orders":{}}`,
		"orders null":       `{"v":1,"type":"orders","seq":2,"run_id":"r","in_reply_to":2,"orders":null}`,
		"order as a string": `{"v":1,"type":"orders","seq":2,"run_id":"r","in_reply_to":2,"orders":["open"]}`,
		"in_reply_to float": `{"v":1,"type":"orders","seq":2,"run_id":"r","in_reply_to":2.0,"orders":[]}`,
	} {
		_, err := Decode([]byte(in))
		require.Error(t, err, name)
	}
}

func TestDecode_Done(t *testing.T) {
	m := decode(t, `{"v":1,"type":"done","seq":812,"run_id":"r-1","status":"completed","reason":""}`).(*Done)

	assert.Equal(t, StatusCompleted, m.Status)
	assert.Equal(t, uint64(812), m.Seq)
	assert.Equal(t, "failed", decode(t, `{"v":1,"type":"done","seq":3,"run_id":"r-1","status":"failed","reason":"x"}`).(*Done).Status)
	assert.Empty(t, decode(t, `{"v":1,"type":"done","seq":3,"run_id":"r-1","status":"cancelled"}`).(*Done).Reason)
}

func TestDecode_DoneRefusals(t *testing.T) {
	for _, in := range []string{
		`{"v":1,"type":"done","seq":3,"run_id":"r-1"}`,
		`{"v":1,"type":"done","seq":3,"run_id":"r-1","status":"ok"}`,
		`{"v":1,"type":"done","seq":3,"run_id":"r-1","status":"Completed"}`,
		`{"v":1,"type":"done","seq":3,"run_id":"r-1","status":"failed","reason":5}`,
	} {
		_, err := Decode([]byte(in))
		require.Error(t, err, in)
	}
}

func TestDecode_Failure(t *testing.T) {
	m := decode(t, `{"v":1,"type":"error","seq":3,"run_id":"r-1","code":"invalid_order","fatal":true,"message":"orders[0]: open requires stop_loss"}`).(*Failure)

	assert.Equal(t, CodeInvalidOrder, m.Code)
	assert.True(t, m.Fatal)
	assert.Equal(t, "orders[0]: open requires stop_loss", m.Message)
}

func TestDecode_AFailureBeforeAcceptedHasNoRunID(t *testing.T) {
	m := decode(t, `{"v":1,"type":"error","seq":1,"code":"version","fatal":true,"message":"v2 is not supported"}`).(*Failure)

	assert.Empty(t, m.RunID)
	assert.Equal(t, CodeVersion, m.Code)
}

func TestDecode_FailureRefusals(t *testing.T) {
	for _, in := range []string{
		`{"v":1,"type":"error","seq":1,"fatal":true,"message":"x"}`,
		`{"v":1,"type":"error","seq":1,"code":"oops","fatal":true,"message":"x"}`,
		`{"v":1,"type":"error","seq":1,"code":"protocol","message":"x"}`,
		`{"v":1,"type":"error","seq":1,"code":"protocol","fatal":"yes","message":"x"}`,
		`{"v":1,"type":"error","seq":1,"code":"protocol","fatal":true}`,
	} {
		_, err := Decode([]byte(in))
		require.Error(t, err, in)
	}
}

// N20 to N28 of ADR-094 that are visible to the decoder.
func TestDecode_EnvelopeRefusals(t *testing.T) {
	for name, tc := range map[string]struct {
		in   string
		code Code
	}{
		"N28 another protocol version": {`{"v":2,"type":"register","seq":1}`, CodeVersion},
		"N28 version 0":                {`{"v":0,"type":"register","seq":1}`, CodeVersion},
		"N28 a version is read first":  {`{"v":2,"type":"nonsense"}`, CodeVersion},
		"no version":                   {`{"type":"register","seq":1}`, CodeProtocol},
		"version as a string":          {`{"v":"1","type":"register","seq":1}`, CodeProtocol},
		"version as a float":           {`{"v":1.0,"type":"register","seq":1}`, CodeProtocol},
		"N26 unknown type":             {`{"v":1,"type":"hello","seq":1,"run_id":"r"}`, CodeProtocol},
		"type in another case":         {`{"v":1,"type":"Register","seq":1}`, CodeProtocol},
		"no type":                      {`{"v":1,"seq":1}`, CodeProtocol},
		"no seq":                       {`{"v":1,"type":"register"}`, CodeProtocol},
		"seq 0":                        {`{"v":1,"type":"register","seq":0}`, CodeProtocol},
		"negative seq":                 {`{"v":1,"type":"register","seq":-1}`, CodeProtocol},
		"seq as a string":              {`{"v":1,"type":"register","seq":"1"}`, CodeProtocol},
		"N24 no run_id on bars":        {`{"v":1,"type":"bars","seq":2}`, CodeProtocol},
		"N24 empty run_id on orders":   {`{"v":1,"type":"orders","seq":2,"run_id":"","in_reply_to":2,"orders":[]}`, CodeProtocol},
		"N24 run_id as a number":       {`{"v":1,"type":"done","seq":2,"run_id":7}`, CodeProtocol},
		"N20 duplicate member":         {`{"v":1,"v":1,"type":"register","seq":1}`, CodeProtocol},
		"N27 not an object":            {`[]`, CodeProtocol},
		"N27 not UTF-8":                {"{\"v\":1,\"type\":\"error\",\"seq\":1,\"code\":\"protocol\",\"fatal\":true,\"message\":\"\xff\"}", CodeProtocol},
		"N27 empty":                    {``, CodeProtocol},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Decode([]byte(tc.in))

			require.Error(t, err)
			assert.Equal(t, tc.code, CodeOf(err), "%v", err)
		})
	}
}

func TestDecode_ADuplicateInsideAnOrderIsAProtocolError(t *testing.T) {
	_, err := Decode([]byte(ordersWith(goodOpen + `,"price":"1.1"`)))

	assert.Equal(t, CodeProtocol, CodeOf(err), "the duplicate is caught before the order is read")
	assert.Contains(t, err.Error(), `"price"`)
}

func TestDecode_ARegisterMayCarryARunIDWhichIsKept(t *testing.T) {
	in := strings.Replace(reg, `"seq":1,`, `"seq":1,"run_id":"r-x",`, 1)

	m := decode(t, in).(*Register)

	assert.Equal(t, "r-x", m.RunID)
}

func TestDecode_BarsKeepsInstrumentOrder(t *testing.T) {
	in := strings.Replace(goodBars, `"bars":[`, `"bars":[{"instrument":"GBPUSD","time":"2024-03-01T10:00:00Z","available_at":"2024-03-01T11:00:00Z","mid":{"open":"1.2","high":"1.2","low":"1.2","close":"1.2"}},`, 1)

	m := decode(t, in).(*Bars)

	require.Len(t, m.Bars, 2)
	assert.Equal(t, []string{"GBPUSD", "EURUSD"}, []string{m.Bars[0].Instrument, m.Bars[1].Instrument}, "delivered as sent")
	assert.Equal(t, marketdata.OHLC{}, m.Bars[0].Bid)
	assert.Zero(t, m.Bars[0].Volume, "volume is optional")
}
