package num

import (
	"encoding/json"
	"fmt"

	"github.com/rustyeddy/trader/num/internal/fixed"
)

// Text and JSON encoding for num's semantic types (ADR-004).
//
// Price, Quantity, and Rate serialize as canonical decimal strings, both as
// text and as JSON strings. Money serializes as a structured JSON value
// carrying an explicit amount and currency; its text form is the single
// canonical string "<amount> <currency>" (one ASCII space), which is a
// distinct, narrower contract used for CSV cells, map keys, and log lines —
// not a substitute for the JSON form.
//
// Writers here always emit canonical text. Readers accept any input the
// underlying exact parser accepts, which includes non-canonical but exactly
// equivalent forms such as trailing zeros; they never round. Nothing in this
// file uses float64.

// MarshalText implements encoding.TextMarshaler.
func (p Price) MarshalText() ([]byte, error) {
	return []byte(p.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (p *Price) UnmarshalText(text []byte) error {
	v, err := ParsePrice(string(text))
	if err != nil {
		return err
	}
	*p = v
	return nil
}

// MarshalJSON implements json.Marshaler, encoding p as a canonical decimal
// string.
func (p Price) MarshalJSON() ([]byte, error) {
	return json.Marshal(p.String())
}

// UnmarshalJSON implements json.Unmarshaler, decoding a JSON string holding
// exact decimal text.
func (p *Price) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	return p.UnmarshalText([]byte(s))
}

// MarshalText implements encoding.TextMarshaler.
func (q Quantity) MarshalText() ([]byte, error) {
	return []byte(q.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (q *Quantity) UnmarshalText(text []byte) error {
	v, err := ParseQuantity(string(text))
	if err != nil {
		return err
	}
	*q = v
	return nil
}

// MarshalJSON implements json.Marshaler, encoding q as a canonical decimal
// string.
func (q Quantity) MarshalJSON() ([]byte, error) {
	return json.Marshal(q.String())
}

// UnmarshalJSON implements json.Unmarshaler, decoding a JSON string holding
// exact decimal text.
func (q *Quantity) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	return q.UnmarshalText([]byte(s))
}

// MarshalText implements encoding.TextMarshaler.
func (r Rate) MarshalText() ([]byte, error) {
	return []byte(r.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (r *Rate) UnmarshalText(text []byte) error {
	v, err := ParseRate(string(text))
	if err != nil {
		return err
	}
	*r = v
	return nil
}

// MarshalJSON implements json.Marshaler, encoding r as a canonical decimal
// string.
func (r Rate) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.String())
}

// UnmarshalJSON implements json.Unmarshaler, decoding a JSON string holding
// exact decimal text.
func (r *Rate) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	return r.UnmarshalText([]byte(s))
}

// MarshalText implements encoding.TextMarshaler.
//
// MarshalText reports ErrInvalidCurrency for an invalid Currency, including
// the zero value, rather than silently writing an empty or malformed code.
func (c Currency) MarshalText() ([]byte, error) {
	if !c.IsValid() {
		return nil, ErrInvalidCurrency
	}
	return []byte(c.code), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (c *Currency) UnmarshalText(text []byte) error {
	v, err := ParseCurrency(string(text))
	if err != nil {
		return err
	}
	*c = v
	return nil
}

// MarshalJSON implements json.Marshaler, encoding c as a JSON string holding
// the currency code.
//
// MarshalJSON reports ErrInvalidCurrency for an invalid Currency, including
// the zero value, rather than silently writing "".
func (c Currency) MarshalJSON() ([]byte, error) {
	if !c.IsValid() {
		return nil, ErrInvalidCurrency
	}
	return json.Marshal(c.code)
}

// UnmarshalJSON implements json.Unmarshaler, decoding a JSON string holding a
// currency code.
func (c *Currency) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	return c.UnmarshalText([]byte(s))
}

// MarshalText implements encoding.TextMarshaler, encoding m as
// "<canonical amount> <currency>", for example "123.45 USD".
//
// MarshalText reports ErrMissingCurrency for invalid Money, since there is no
// canonical text for money that is not self-describing.
func (m Money) MarshalText() ([]byte, error) {
	if !m.IsValid() {
		return nil, ErrMissingCurrency
	}
	return []byte(m.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler, decoding the
// "<amount> <currency>" form produced by MarshalText. Exactly one ASCII space
// separates the two fields.
func (m *Money) UnmarshalText(text []byte) error {
	s := string(text)
	i := indexByte(s, ' ')
	if i < 0 {
		return fmt.Errorf("%w: missing currency separator", ErrSyntax)
	}
	if indexByte(s[i+1:], ' ') >= 0 {
		return fmt.Errorf("%w: too many fields", ErrSyntax)
	}

	amount, currencyCode := s[:i], s[i+1:]

	var currency Currency
	if err := currency.UnmarshalText([]byte(currencyCode)); err != nil {
		return err
	}

	v, err := ParseMoney(amount, currency)
	if err != nil {
		return err
	}
	*m = v
	return nil
}

func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

// moneyJSON is Money's structured JSON wire form (ADR-004): an explicit
// canonical decimal amount alongside an explicit currency, never a bare
// number and never a raw scaled integer.
type moneyJSON struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

// MarshalJSON implements json.Marshaler, encoding m as
// {"amount":"123.45","currency":"USD"}.
//
// MarshalJSON reports ErrMissingCurrency for invalid Money.
func (m Money) MarshalJSON() ([]byte, error) {
	if !m.IsValid() {
		return nil, ErrMissingCurrency
	}
	return json.Marshal(moneyJSON{
		Amount:   fixed.Format(m.amount),
		Currency: m.currency.String(),
	})
}

// UnmarshalJSON implements json.Unmarshaler, decoding the structured
// {"amount":"...","currency":"..."} form.
func (m *Money) UnmarshalJSON(data []byte) error {
	var wire moneyJSON
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}

	var currency Currency
	if err := currency.UnmarshalText([]byte(wire.Currency)); err != nil {
		return err
	}

	v, err := ParseMoney(wire.Amount, currency)
	if err != nil {
		return err
	}
	*m = v
	return nil
}
