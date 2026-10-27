package main

type broker struct {
	id       string
	accounts map[string]*account
}

func getBroker(id string) (*broker, bool) {
	return &broker{
		id: id,
	}, true
}

func (b *broker) addAccount(id string, balance money) *account {
	act := newAccount(id, balance)
	b.accounts[id] = act
	return act
}

func (b *broker) getAccount(id string) (*account, bool) {
	a, ok := b.accounts[id]
	return a, ok
}
