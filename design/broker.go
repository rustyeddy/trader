package main

type broker struct {
	id       string
	accounts map[string]*account
}

var brokers map[string]*broker

func init() {
	brokers = make(map[string]*broker)
	brokers["sim"] = &broker{"sim", nil}
	brokers["sim"].addAccount("sim123", 10000)
}

func getBroker(id string) (br *broker, ok bool) {
	if br, ok = brokers[id]; !ok {
		brokers[id] = &broker{id, make(map[string]*account)}
		br = brokers[id]
	}
	return br, true
}

func (b *broker) addAccount(id string, balance money) *account {
	if b.accounts == nil {
		b.accounts = make(map[string]*account)
	}
	act := newAccount(id, balance)
	b.accounts[id] = act
	return act
}

func (b *broker) getAccount(id string) (*account, bool) {
	a, ok := b.accounts[id]
	return a, ok
}
