//go:build !sqlite

package trader

import "errors"

// NewSQLite is a stub that returns an error when the binary is built without
// the sqlite build tag. To enable SQLite support: go build -tags sqlite.
func NewSQLite(_ string) (Journal, error) {
	return nil, errors.New("SQLite support not compiled in; rebuild with -tags sqlite")
}
