package account

import "github.com/rustyeddy/trader/config/active"

// Selection is the CLI's locally-persisted default broker/account choice.
type Selection = active.Selection

// DefaultSelection reads the CLI's locally-persisted default broker/account.
func DefaultSelection() (Selection, error) {
	return active.Load()
}

// SetDefault persists broker/accountID as the CLI's default selection.
func SetDefault(broker, accountID string) error {
	return active.Save(active.Selection{Broker: broker, AccountID: accountID})
}
