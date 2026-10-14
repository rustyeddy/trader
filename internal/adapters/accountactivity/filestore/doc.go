// Package filestore is the file-backed accountactivity.Store (issue #509,
// ADR-079): a peer adapter to the journal's JSONL adapter, with no new
// dependency. A future SQL adapter would sit beside it and pass the same
// storetest contract.
//
// # Layout
//
//	<root>/<provider>/<environment>/<account>/activities.jsonl
//	<root>/<provider>/<environment>/<account>/checkpoint.json
//
// One directory per stream (accountactivity.SyncKey). activities.jsonl holds
// one activity per line in accountactivity.Compare order, so a read is
// already sorted and the file is deterministic for a given set of
// activities. Path components are escaped, and "." and ".." are refused, so
// a provider account ID cannot name a path outside the root.
//
// # Durability and ordering
//
// Both files are replaced whole: written to a temporary file in the same
// directory, fsynced, then renamed over the old one, so a crash leaves
// either the old file or the new one, never a torn one. Apply writes
// activities first and the checkpoint second. A failure between the two
// leaves the new activities stored and the checkpoint unmoved; retrying the
// same batch finds them already stored and then advances the checkpoint.
// The checkpoint therefore never points past activities that are not
// durable.
//
// # Concurrency
//
// A Store serializes Apply per stream within one process. It does not lock
// across processes: one importer process per root is assumed, as with the
// market-data store.
package filestore
