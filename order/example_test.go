package order_test

import (
	"fmt"
	"time"

	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

func exampleListing() instrument.Listing {
	inst, err := instrument.NewCurrencyPair(num.MustParseCurrency("EUR"), num.MustParseCurrency("USD"))
	if err != nil {
		panic(err)
	}
	spec, err := instrument.NewSpec(
		num.MustParsePrice("0.00001"),
		num.MustParseQuantity("1"),
		num.MustParseRate("1"),
		num.MustParseCurrency("USD"),
	)
	if err != nil {
		panic(err)
	}
	listing, err := instrument.NewListing(instrument.ListingParams{
		Instrument: inst,
		Provider:   "OANDA",
		Symbol:     "EUR_USD",
		Spec:       spec,
		Tradable:   true,
	})
	if err != nil {
		panic(err)
	}
	return listing
}

// Example_proposalToRequest shows the proposal-to-request stage: a
// Proposal is validated on its own, then assigned a Trader OrderID to
// become a Request ready for submission.
func Example_proposalToRequest() {
	g := id.NewGenerator(clock.NewSimulated(time.Now()), id.NewDeterministic(1, 2))
	accountID, err := id.GenerateAccountID(g)
	if err != nil {
		panic(err)
	}

	proposal, err := order.NewProposal(order.Proposal{
		Listing:     exampleListing(),
		AccountID:   accountID,
		Side:        order.Buy,
		Type:        order.Market,
		TimeInForce: order.GTC,
		Quantity:    num.MustParseQuantity("1000"),
	})
	if err != nil {
		panic(err)
	}

	orderID, err := id.GenerateOrderID(g)
	if err != nil {
		panic(err)
	}
	request, err := order.NewRequest(proposal, orderID)
	if err != nil {
		panic(err)
	}

	fmt.Println(request.Side, request.Type, request.Quantity)
	// Output:
	// buy market 1000
}

// ExampleOrder_RemainingQuantity shows that remaining quantity derives
// from the broker-accepted quantity, not the originally requested one —
// a broker may accept a normalized quantity that differs from what was
// requested.
func ExampleOrder_RemainingQuantity() {
	g := id.NewGenerator(clock.NewSimulated(time.Now()), id.NewDeterministic(1, 2))
	accountID, err := id.GenerateAccountID(g)
	if err != nil {
		panic(err)
	}
	proposal, err := order.NewProposal(order.Proposal{
		Listing:     exampleListing(),
		AccountID:   accountID,
		Side:        order.Buy,
		Type:        order.Market,
		TimeInForce: order.GTC,
		Quantity:    num.MustParseQuantity("1000"),
	})
	if err != nil {
		panic(err)
	}
	orderID, err := id.GenerateOrderID(g)
	if err != nil {
		panic(err)
	}
	request, err := order.NewRequest(proposal, orderID)
	if err != nil {
		panic(err)
	}

	accepted := num.MustParseQuantity("1000")
	o, err := order.NewOrder(order.Order{
		Request:          request,
		AcceptedQuantity: &accepted,
		FilledQuantity:   num.MustParseQuantity("400"),
		Status:           order.StatusPartiallyFilled,
	})
	if err != nil {
		panic(err)
	}

	remaining, err := o.RemainingQuantity()
	if err != nil {
		panic(err)
	}
	fmt.Println(remaining)
	// Output:
	// 600
}

// Example_unknownBrokerStatus shows that an unrecognized broker status
// or rejection code becomes the safe Unknown value rather than a crash
// or a guess, while the broker's own text is preserved.
func Example_unknownBrokerStatus() {
	// parseBrokerStatus stands in for an adapter's own mapping from a
	// broker's status vocabulary to order.Status.
	parseBrokerStatus := func(brokerText string) order.Status {
		switch brokerText {
		case "OPEN":
			return order.StatusWorking
		default:
			return order.StatusUnknown
		}
	}

	fmt.Println(parseBrokerStatus("OPEN"))
	fmt.Println(parseBrokerStatus("SOME_NEW_BROKER_STATE"))
	// Output:
	// working
	// unknown
}
