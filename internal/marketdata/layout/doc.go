// Package layout is the one resolver for where persisted price data lives
// (ADR-089, docs/arch/market-data-storage.md). Every path has the form
//
//	<root>/<source>/<stage>/<instrument>/<YYYY>/<MM>/<instrument>-<YYYY>-<MM>-<interval>.csv
//
// and is built and parsed only here, so the command line, services, ingestion
// and tests cannot each invent their own and path traversal is rejected in a
// single place.
//
// The package imports only the standard library. It never touches the
// filesystem and never falls back to another source, stage or the retired
// <stage>/<source> order: a caller that cannot find the partition at the path
// returned here reports that path.
//
// Which month a bar belongs to is the provider package's rule, not this one's:
// intraday partitions are by UTC opening time, and a daily partition keeps the
// month of the file it is stored in (ADR-089).
package layout
