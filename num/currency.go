package num

// Currency identifies the denomination of a Money value.
//
// Currency validation is structural only: 3 to 5 uppercase ASCII letters.
// This is deliberate — num does not embed an ISO-4217 table, a crypto asset
// registry, or any other authoritative list of supported currencies.
// Instrument and venue packages own that authority; num only guarantees that
// whatever code is stored here is a well-formed identifier, not that it is a
// currency Trader actually supports or trades.
//
// Currency carries no minor-unit count, display precision, tick size, or
// other presentation convention. Those are presentation and instrument
// concerns, not properties of the identifier itself.
//
// The Go zero value of Currency is invalid: it has no code and fails
// validation. A valid Currency must be constructed with ParseCurrency or
// MustParseCurrency.
type Currency struct {
	code string
}

// ParseCurrency validates code as a currency identifier: 3 to 5 uppercase
// ASCII letters, nothing else. Empty input, lowercase letters, digits,
// punctuation, separators, and whitespace are all rejected.
func ParseCurrency(code string) (Currency, error) {
	if !isValidCurrencyCode(code) {
		return Currency{}, ErrInvalidCurrency
	}
	return Currency{code: code}, nil
}

// MustParseCurrency is like ParseCurrency but panics on error.
//
// It is intended for programmer-controlled constants, fixtures, and tests,
// not for parsing external or market input.
func MustParseCurrency(code string) Currency {
	c, err := ParseCurrency(code)
	if err != nil {
		panic(err)
	}
	return c
}

// IsValid reports whether c holds a structurally valid currency code.
//
// The Go zero value of Currency reports false.
func (c Currency) IsValid() bool {
	return isValidCurrencyCode(c.code)
}

// String returns the currency code, or the empty string for the zero value.
func (c Currency) String() string {
	return c.code
}

// Equal reports whether c and o hold the identical currency code.
func (c Currency) Equal(o Currency) bool {
	return c.code == o.code
}

func isValidCurrencyCode(code string) bool {
	if len(code) < 3 || len(code) > 5 {
		return false
	}
	for i := 0; i < len(code); i++ {
		if code[i] < 'A' || code[i] > 'Z' {
			return false
		}
	}
	return true
}
