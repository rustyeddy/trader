package tradertest_test

import (
	"testing"

	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/rustyeddy/trader/tradertest"
	"github.com/stretchr/testify/assert"
)

// mockT records whether Errorf was called, without depending on
// *testing.T's own pass/fail bookkeeping, so we can assert that a
// tradertest assertion reports failure without actually failing this
// test.
type mockT struct {
	failed bool
}

func (m *mockT) Errorf(format string, args ...any) { m.failed = true }

func TestAssertMoneyEqual(t *testing.T) {
	usd := num.MustParseCurrency("USD")
	a := num.MustParseMoney("100", usd)
	b := num.MustParseMoney("100", usd)
	c := num.MustParseMoney("200", usd)

	assert.True(t, tradertest.AssertMoneyEqual(t, a, b))

	m := &mockT{}
	tradertest.AssertMoneyEqual(m, a, c)
	assert.True(t, m.failed)
}

func TestAssertPriceEqual(t *testing.T) {
	a := num.MustParsePrice("1.10000")
	b := num.MustParsePrice("1.10000")
	c := num.MustParsePrice("1.20000")

	assert.True(t, tradertest.AssertPriceEqual(t, a, b))

	m := &mockT{}
	tradertest.AssertPriceEqual(m, a, c)
	assert.True(t, m.failed)
}

func TestAssertQuantityEqual(t *testing.T) {
	a := num.MustParseQuantity("100")
	b := num.MustParseQuantity("100")
	c := num.MustParseQuantity("200")

	assert.True(t, tradertest.AssertQuantityEqual(t, a, b))

	m := &mockT{}
	tradertest.AssertQuantityEqual(m, a, c)
	assert.True(t, m.failed)
}

func TestAssertRateEqual(t *testing.T) {
	a := num.MustParseRate("1.1")
	b := num.MustParseRate("1.1")
	c := num.MustParseRate("1.2")

	assert.True(t, tradertest.AssertRateEqual(t, a, b))

	m := &mockT{}
	tradertest.AssertRateEqual(m, a, c)
	assert.True(t, m.failed)
}

func TestAssertStatus(t *testing.T) {
	o := mustWorkingOrder(t)

	assert.True(t, tradertest.AssertStatus(t, order.StatusWorking, o))

	m := &mockT{}
	tradertest.AssertStatus(m, order.StatusFilled, o)
	assert.True(t, m.failed)
}

func TestAssertTerminal(t *testing.T) {
	g := testGenerator()
	o := mustWorkingOrder(t)
	f := tradertest.MustNewFillFor(tradertest.FillParams{
		Order:  o,
		FillID: tradertest.MustFillID(g),
	})
	filled, err := order.ApplyFill(o, f)
	assert.NoError(t, err)

	assert.True(t, tradertest.AssertTerminal(t, filled))

	m := &mockT{}
	tradertest.AssertTerminal(m, o)
	assert.True(t, m.failed)
}
