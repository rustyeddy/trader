package tradertest

import (
	"fmt"

	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/stretchr/testify/assert"
)

// AssertMoneyEqual asserts that want and got are the same amount in the
// same currency, using num.Money.Equal rather than assert.Equal's
// struct-reflection comparison: Money.Equal treats a currency mismatch
// as simply unequal, not a comparison error, which is the semantics a
// domain-aware assertion should report.
func AssertMoneyEqual(t assert.TestingT, want, got num.Money, msgAndArgs ...any) bool {
	helper(t)
	if want.Equal(got) {
		return true
	}
	return assert.Fail(t, fmt.Sprintf("Money not equal:\nwant: %s\ngot:  %s", want, got), msgAndArgs...)
}

// AssertPriceEqual asserts that want and got are the same exact price,
// using num.Price.Equal.
func AssertPriceEqual(t assert.TestingT, want, got num.Price, msgAndArgs ...any) bool {
	helper(t)
	if want.Equal(got) {
		return true
	}
	return assert.Fail(t, fmt.Sprintf("Price not equal:\nwant: %s\ngot:  %s", want, got), msgAndArgs...)
}

// AssertQuantityEqual asserts that want and got are the same exact
// quantity, using num.Quantity.Equal.
func AssertQuantityEqual(t assert.TestingT, want, got num.Quantity, msgAndArgs ...any) bool {
	helper(t)
	if want.Equal(got) {
		return true
	}
	return assert.Fail(t, fmt.Sprintf("Quantity not equal:\nwant: %s\ngot:  %s", want, got), msgAndArgs...)
}

// AssertRateEqual asserts that want and got are the same exact rate,
// using num.Rate.Equal.
func AssertRateEqual(t assert.TestingT, want, got num.Rate, msgAndArgs ...any) bool {
	helper(t)
	if want.Equal(got) {
		return true
	}
	return assert.Fail(t, fmt.Sprintf("Rate not equal:\nwant: %s\ngot:  %s", want, got), msgAndArgs...)
}

// AssertStatus asserts that o's Status is want.
func AssertStatus(t assert.TestingT, want order.Status, o order.Order, msgAndArgs ...any) bool {
	helper(t)
	if o.Status == want {
		return true
	}
	return assert.Fail(t, fmt.Sprintf("Order status not equal:\nwant: %s\ngot:  %s", want, o.Status), msgAndArgs...)
}

// AssertTerminal asserts that o's Status is a terminal one (see
// order.Status.Terminal): StatusFilled, StatusCanceled, StatusRejected,
// or StatusExpired.
func AssertTerminal(t assert.TestingT, o order.Order, msgAndArgs ...any) bool {
	helper(t)
	if o.Status.Terminal() {
		return true
	}
	return assert.Fail(t, fmt.Sprintf("Order status %s is not terminal", o.Status), msgAndArgs...)
}

// helper marks the calling assertion as a test helper when t supports
// it (as *testing.T and *testing.B do), so failure line numbers point
// at the caller rather than into tradertest.
func helper(t assert.TestingT) {
	if h, ok := t.(interface{ Helper() }); ok {
		h.Helper()
	}
}
