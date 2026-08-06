package id

import "encoding/json"

// MarshalText implements encoding.TextMarshaler, encoding id as its
// canonical text form.
//
// MarshalText reports ErrZeroValue for the zero value rather than emitting
// an empty or placeholder string: the zero value is not a real identifier,
// and a caller that forgot to set one deserves an error here, not a
// silently empty field on the wire.
func (id ID[K]) MarshalText() ([]byte, error) {
	if !id.set {
		return nil, ErrZeroValue
	}
	return []byte(id.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (id *ID[K]) UnmarshalText(text []byte) error {
	v, err := Parse[K](string(text))
	if err != nil {
		return err
	}
	*id = v
	return nil
}

// MarshalJSON implements json.Marshaler, encoding id as a JSON string
// holding its canonical text form.
//
// MarshalJSON reports ErrZeroValue for the zero value, for the same reason
// as MarshalText.
func (id ID[K]) MarshalJSON() ([]byte, error) {
	if !id.set {
		return nil, ErrZeroValue
	}
	return json.Marshal(id.String())
}

// UnmarshalJSON implements json.Unmarshaler, decoding a JSON string holding
// an identifier's canonical text form.
func (id *ID[K]) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	return id.UnmarshalText([]byte(s))
}
