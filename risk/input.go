package risk

import (
	"fmt"
	"strings"

	"github.com/rustyeddy/trader/order"
)

// checkInput validates in's fields and returns the fully revalidated
// Proposal. Malformed input must never reach a Rule (issue #180's own
// review): each Rule assumes Proposal is already structurally sound
// and consistent with Account, exactly the way order.NewRequest
// revalidates its embedded Proposal rather than trusting it by
// provenance.
func checkInput(in Input) (order.Proposal, error) {
	validProposal, err := order.NewProposal(in.Proposal)
	if err != nil {
		return order.Proposal{}, fmt.Errorf("%w: proposal: %v", ErrInvalidInput, err)
	}
	if in.Account.AccountID().IsZero() {
		return order.Proposal{}, fmt.Errorf("%w: account must be constructed", ErrInvalidInput)
	}
	if !validProposal.AccountID.Equal(in.Account.AccountID()) {
		return order.Proposal{}, fmt.Errorf("%w: proposal account id %s does not match account %s",
			ErrInvalidInput, validProposal.AccountID, in.Account.AccountID())
	}
	if !strings.EqualFold(validProposal.Listing.Provider(), in.Account.Broker()) {
		return order.Proposal{}, fmt.Errorf("%w: listing provider %q does not match account broker %q",
			ErrInvalidInput, validProposal.Listing.Provider(), in.Account.Broker())
	}
	return validProposal, nil
}
