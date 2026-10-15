package accountactivity

import (
	"time"

	"github.com/rustyeddy/trader/num"
)

// PriceLevel is one rung of a quoted liquidity ladder: a price and the
// quantity available at it. Both must be positive in a valid quote: a rung
// with no liquidity is not a price anyone could trade at.
type PriceLevel struct {
	Price     num.Price
	Liquidity num.Quantity
}

// ExecutionQuote is the broker's quote and liquidity snapshot a fill was
// priced from (OANDA fullPrice): the bid and ask ladders with the liquidity
// at each rung, and the closeout bid and ask. The ladders are kept whole,
// not just the top of the book, because a fill larger than the first rung
// walks the ladder and the cost of that cannot be reproduced from the best
// price alone. It is a reported market snapshot, not slippage; choosing a
// benchmark from it and recording the derivation is later work.
//
// Ladders are in the order the broker gave them (best price first). Every
// part is optional; an ExecutionQuote with none of them is not valid.
type ExecutionQuote struct {
	// At is when the broker timestamped the quote. Zero if not supplied.
	At          time.Time
	Bids, Asks  []PriceLevel
	CloseoutBid Value[num.Price]
	CloseoutAsk Value[num.Price]
}
