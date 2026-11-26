// Package rest is the HTTP transport for the canonical market-data history (ADR-091):
// a thin adapter, like cmd/trader, that normalizes the parameters of a request, calls
// the application service and writes what comes back. It holds no data access and no
// rule: reading the bars, paging them, the policy for what is missing and the report
// of what is missing are all service.ReadBars. It imports the application service, the
// public marketdata model and the standard library, never a provider, the domain
// service, the store or the layout; a test enforces that.
//
// The one endpoint is GET /v1/marketdata/bars, documented in docs/UsersGuide.md. The
// schema is provider-neutral: the source is a plain name ("oanda" or "stooq"), and nothing about
// credentials or provider records appears in a request or a response.
package rest
