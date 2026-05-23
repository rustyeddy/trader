package trader

// AccountManager is a simple registry of named accounts. It is used by the
// Trader to look up accounts by name or ID during order processing.
type AccountManager struct {
	accounts map[string]*Account
}

// NewAccountManager returns an empty AccountManager.
func NewAccountManager() *AccountManager {
	return &AccountManager{
		accounts: make(map[string]*Account),
	}
}

// CreateAccount creates a new Account with the given name and a deposit of b
// whole currency units (i.e. b × MoneyScale micro-units), stores it by name,
// and returns it.
func (am *AccountManager) CreateAccount(name string, b int64) *Account {
	balance := Money(b * int64(MoneyScale))
	act := NewAccount(name, balance)
	am.accounts[name] = act
	return act
}

// Add registers an existing account, keyed by its ID.
func (am *AccountManager) Add(act *Account) {
	am.accounts[act.ID] = act
}

// Get returns the account registered under name, or nil if not found.
func (am *AccountManager) Get(name string) *Account {
	act, _ := am.accounts[name]
	return act
}
