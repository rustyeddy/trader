package proto

import (
	"testing"
	"time"

	"github.com/rustyeddy/trader/strategy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func goodRegister() *Register {
	return &Register{Header: Header{Seq: 1}, Name: "emax", Capabilities: []string{"bars", "warm"},
		Run: Run{Mode: ModeBacktest, Interval: "H1", Instruments: []string{"EURUSD", "GBPUSD"},
			From: ts("2024-03-01T00:00:00Z"), To: ts("2024-04-01T00:00:00Z"), WarmBars: 100}}
}

func TestRegister_Validate_Accepts(t *testing.T) {
	require.NoError(t, goodRegister().Validate())

	paper := goodRegister()
	paper.Run.Mode, paper.Run.To = ModePaper, time.Time{}
	require.NoError(t, paper.Validate(), "a paper run needs no end")

	d1 := goodRegister()
	d1.Run.Interval, d1.Run.WarmBars, d1.Capabilities = "D1", 0, []string{"bars"}
	require.NoError(t, d1.Validate())
}

func TestRegister_Validate_Refusals(t *testing.T) {
	for name, edit := range map[string]func(*Register){
		"no name":                        func(m *Register) { m.Name = "" },
		"no bars capability":             func(m *Register) { m.Capabilities = []string{"warm"} },
		"unknown capability":             func(m *Register) { m.Capabilities = []string{"bars", "fills"} },
		"repeated capability":            func(m *Register) { m.Capabilities = []string{"bars", "bars"} },
		"live is reserved":               func(m *Register) { m.Run.Mode = ModeLive },
		"unknown mode":                   func(m *Register) { m.Run.Mode = "replay" },
		"unknown interval":               func(m *Register) { m.Run.Interval = "M5" },
		"interval in another case":       func(m *Register) { m.Run.Interval = "h1" },
		"no instruments":                 func(m *Register) { m.Run.Instruments = nil },
		"empty instrument":               func(m *Register) { m.Run.Instruments = []string{""} },
		"instrument with a space":        func(m *Register) { m.Run.Instruments = []string{"EUR USD"} },
		"instrument with a newline":      func(m *Register) { m.Run.Instruments = []string{"EURUSD\n"} },
		"repeated instrument":            func(m *Register) { m.Run.Instruments = []string{"EURUSD", "EURUSD"} },
		"no start":                       func(m *Register) { m.Run.From = time.Time{} },
		"backtest without an end":        func(m *Register) { m.Run.To = time.Time{} },
		"end equal to the start":         func(m *Register) { m.Run.To = m.Run.From },
		"end before the start":           func(m *Register) { m.Run.To = m.Run.From.Add(-time.Hour) },
		"warm-up without the capability": func(m *Register) { m.Capabilities = []string{"bars"} },
	} {
		t.Run(name, func(t *testing.T) {
			m := goodRegister()
			edit(m)

			err := m.Validate()

			require.Error(t, err)
			assert.Equal(t, CodeRejected, CodeOf(err), "%v", err)
		})
	}
}

func TestRegister_Validate_APaperRunWithAnEndBeforeTheStartIsRefused(t *testing.T) {
	m := goodRegister()
	m.Run.Mode = ModePaper
	m.Run.To = m.Run.From.Add(-time.Hour)

	assert.Equal(t, CodeRejected, CodeOf(m.Validate()))
}

func TestOrders_Validate(t *testing.T) {
	frame := ts("2024-03-01T11:00:00Z")
	good := strategy.Order{Instrument: "EURUSD", Action: strategy.Open, Direction: strategy.Long, Price: pr("1.1"), StopLoss: pr("1.09")}

	require.NoError(t, (&Orders{}).Validate(frame), "no orders is a legal reply")
	require.NoError(t, (&Orders{Orders: []strategy.Order{good, {Instrument: "EURUSD", Action: strategy.Close}}}).Validate(frame))

	bad := good
	bad.StopLoss = pr("1.2")
	err := (&Orders{Orders: []strategy.Order{good, bad}}).Validate(frame)

	require.Error(t, err)
	assert.Equal(t, CodeInvalidOrder, CodeOf(err))
	assert.Contains(t, err.Error(), "orders[1]", "the order is named")
	assert.Contains(t, err.Error(), "stop_loss")
}

func TestOrders_Validate_ChecksExpiryAgainstTheFrame(t *testing.T) {
	frame := ts("2024-03-01T11:00:00Z")
	o := strategy.Order{Instrument: "EURUSD", Action: strategy.Open, Direction: strategy.Long, Price: pr("1.1"),
		StopLoss: pr("1.09"), Expires: frame}

	assert.Equal(t, CodeInvalidOrder, CodeOf((&Orders{Orders: []strategy.Order{o}}).Validate(frame)))
	o.Expires = frame.Add(time.Hour)
	assert.NoError(t, (&Orders{Orders: []strategy.Order{o}}).Validate(frame))
}
