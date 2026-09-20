package alpaca

import "context"

// CredentialProvider supplies the two secrets Alpaca's API requires —
// a key ID and a secret key, sent as separate headers, unlike OANDA's
// single bearer token (oanda.CredentialProvider) — that Client
// authenticates requests with. Credentials is called before every
// request Client makes, including each page of a paginated fetch and
// each retry, rather than once and cached, so an implementation may
// rotate or refresh credentials transparently (oanda.CredentialProvider's
// own reasoning, applied to Alpaca's two-secret shape).
//
// A CredentialProvider must not log, wrap in an error message, or
// otherwise expose either secret it returns; Client itself never does
// (see client.go).
type CredentialProvider interface {
	Credentials(ctx context.Context) (keyID, secretKey string, err error)
}

// StaticCredential is the simplest CredentialProvider: a fixed key
// ID/secret key pair supplied once. It exists for composition roots
// and tests that do not need rotation; it is not a recommendation to
// store long-lived secrets in a Go value any more casually than the
// composition root already must.
type StaticCredential struct {
	KeyID     string
	SecretKey string
}

// Credentials implements CredentialProvider.
func (s StaticCredential) Credentials(context.Context) (string, string, error) {
	return s.KeyID, s.SecretKey, nil
}
