// Package accountactivity is the application service that runs the
// provider-neutral account-activity import (issue #510, ADR-081):
//
//	checkpoint -> fetch page -> stamp -> Apply (store + checkpoint) -> repeat
//
// It knows nothing about any provider. A provider supplies an
// accountactivity.Source; durable storage is an accountactivity.Store. A
// failure at any step leaves the checkpoint at the last fully stored page, so
// rerunning Sync converges without duplicates.
package accountactivity

import (
	"context"
	"errors"
	"fmt"

	"github.com/rustyeddy/trader/internal/accountactivity"
	"github.com/rustyeddy/trader/internal/clock"
)

// Params are the collaborators of one Sync run.
type Params struct {
	Store  accountactivity.Store
	Source accountactivity.Source
	// Clock stamps ImportedAt and the checkpoint time. It is injected so a
	// run is deterministic; Sync never reads the wall clock.
	Clock clock.Clock
}

// Result summarizes a Sync run.
type Result struct {
	// Pages is how many pages were applied.
	Pages int
	// Fetched is how many activities the source returned.
	Fetched int
	// Added is how many were new to the store; Unchanged were already stored.
	Added, Unchanged int
	// Other is how many fetched activities were KindOther: provider events
	// kept for provenance but not modelled economically.
	Other int
	// Cursor is the checkpoint after the run.
	Cursor string
}

// ErrNoProgress reports a source that returned a page that neither advanced
// the cursor nor finished, which would loop forever.
var ErrNoProgress = errors.New("accountactivity: source made no progress")

// Sync imports everything the source has beyond the stream's checkpoint.
// It returns the counts so far and the error if a step fails; the
// checkpoint is never ahead of stored activities.
func Sync(ctx context.Context, p Params) (Result, error) {
	var res Result
	if p.Store == nil || p.Source == nil || p.Clock == nil {
		return res, errors.New("accountactivity: store, source and clock are required")
	}
	stream := p.Source.Stream()
	if err := stream.Validate(); err != nil {
		return res, fmt.Errorf("accountactivity: source stream: %w", err)
	}

	cp, ok, err := p.Store.Checkpoint(ctx, stream)
	if err != nil {
		return res, fmt.Errorf("accountactivity: reading checkpoint: %w", err)
	}
	cursor := ""
	if ok {
		cursor = cp.Cursor
	}
	res.Cursor = cursor

	for {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		page, err := p.Source.Next(ctx, cursor)
		if err != nil {
			return res, fmt.Errorf("accountactivity: fetching after %q: %w", cursor, err)
		}

		if len(page.Activities) == 0 && page.Cursor == cursor {
			if page.Done {
				return res, nil
			}
			return res, fmt.Errorf("%w at cursor %q", ErrNoProgress, cursor)
		}
		if page.Cursor == cursor && !page.Done {
			return res, fmt.Errorf("%w at cursor %q", ErrNoProgress, cursor)
		}

		now := p.Clock.Now()
		for i := range page.Activities {
			page.Activities[i].ImportedAt = now
			if page.Activities[i].Kind == accountactivity.KindOther {
				res.Other++
			}
		}
		applied, err := p.Store.Apply(ctx, accountactivity.Batch{
			Sync: stream, Activities: page.Activities, Cursor: page.Cursor, At: now,
		})
		if err != nil {
			return res, fmt.Errorf("accountactivity: storing page ending at %q: %w", page.Cursor, err)
		}
		res.Pages++
		res.Fetched += len(page.Activities)
		res.Added += applied.Added
		res.Unchanged += applied.Unchanged
		cursor = page.Cursor
		res.Cursor = cursor
		if page.Done {
			return res, nil
		}
	}
}
