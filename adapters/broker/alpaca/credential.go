package alpaca

import "context"

// CredentialProvider supplies the two secrets Alpaca's Trading API
// requires — a key ID and a secret key, sent as separate headers —
// matching marketdata/internal/provider/alpaca.CredentialProvider's
// identical shape (duplicated, not imported, per the package doc
// comment). Credentials is called before every request Client makes,
// including each retry, so an implementation may rotate credentials
// transparently.
//
// A CredentialProvider must not log, wrap in an error message, or
// otherwise expose either secret it returns; Client itself never does
// (see client.go).
type CredentialProvider interface {
	Credentials(ctx context.Context) (keyID, secretKey string, err error)
}

// StaticCredential is the simplest CredentialProvider: a fixed key
// ID/secret key pair supplied once. It exists for composition roots and
// tests that do not need rotation.
type StaticCredential struct {
	KeyID     string
	SecretKey string
}

// Credentials implements CredentialProvider.
func (s StaticCredential) Credentials(context.Context) (string, string, error) {
	return s.KeyID, s.SecretKey, nil
}
