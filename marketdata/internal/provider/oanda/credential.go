package oanda

import "context"

// CredentialProvider supplies the OANDA API bearer token Client
// authenticates requests with (issue #80, ADR-020). It is intentionally
// minimal — Client asks for a token, nothing more — so an implementation
// (a static token, a value resolved from the composition root's own
// configuration, a secrets-manager client, a rotating credential) can
// vary freely without Client, or marketdata.Manager, ever caring which.
//
// Token is called before every request Client makes — including each
// page of a paginated fetch and each retry — rather than once and
// cached, so an implementation may rotate or refresh a token
// transparently. A CredentialProvider must not log, wrap in an error
// message, or otherwise expose the token it returns; Client itself never
// does (see client.go).
type CredentialProvider interface {
	Token(ctx context.Context) (string, error)
}

// StaticCredential is the simplest CredentialProvider: a fixed token
// supplied once. It exists for composition roots and tests that do not
// need rotation; it is not a recommendation to store a long-lived token
// in a Go value any more casually than the composition root already
// must.
type StaticCredential string

// Token implements CredentialProvider.
func (s StaticCredential) Token(context.Context) (string, error) {
	return string(s), nil
}
