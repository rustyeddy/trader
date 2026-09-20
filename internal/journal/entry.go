package journal

// Entry is a Record together with the Sequence a Recorder assigned it
// once durably recorded. It is the only value a Reader ever produces —
// there is no way to construct one directly, matching Record's own
// doc comment about sequence ownership.
type Entry struct {
	Record
	// Sequence is this Entry's position in its Recorder's own append
	// order: strictly increasing, starting at 1, with no gaps for a
	// non-corrupt journal. It is assigned by the Recorder, never by a
	// caller of Record.
	Sequence uint64
}
