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
// directory, fsynced, renamed over the old one, and the directory is then
// fsynced, so a crash leaves either the old file or the new one, never a
// torn one. A failed directory sync is an error, never ignored. The
// whole directory chain from the filesystem root down to the stream is fsynced,
// including directories that already exist, because a directory left by an
// earlier attempt whose parent sync failed looks identical to a stable one.
// Successful syncs are remembered per process and failures never are, so a
// retry re-establishes what failed and a stream cannot vanish after Apply
// returned success. Ancestors above the store root that the user may not be
// able to open are tolerated on permission errors only.
// When a retry finds every activity already stored and so writes nothing,
// Apply still fsyncs the activities file and its directory before moving the
// checkpoint: it establishes durability rather than assuming it. Apply writes
// activities first and the checkpoint second. A failure between the two
// leaves the new activities stored and the checkpoint unmoved; retrying the
// same batch finds them already stored and then advances the checkpoint.
// The checkpoint therefore never points past activities that are not
// durable.
//
// # Record size
//
// One encoded activity line must be under 8 MiB (maxLineBytes), the same
// bound the reader uses. The writer rejects a larger record with
// ErrRecordTooLarge before publishing anything, so the store never holds a
// record it cannot read back.
//
// # Reading
//
// A value object with no payload, a checkpoint with an empty cursor or a
// zero time, and any malformed or foreign line are corruption and are
// reported. An explicitly encoded zero is a valid value.
//
// # Concurrency
//
// A Store serializes Apply per stream within one process. It does not lock
// across processes: one importer process per root is assumed, as with the
// market-data store.
package filestore
