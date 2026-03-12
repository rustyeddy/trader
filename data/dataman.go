package data

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
)

// DataManager is responsible for identifing data files that are
// missing accross all instruments. For missing datasets, ensure they
// are downloaded, for datasets that are downloaded, make sure they
// are made into candles.
type DataManager struct {
	Start       time.Time
	End         time.Time
	Basedir     string
	Instruments []string

	DukasRoot  string
	Candlesdir string

	*Downloader
	Store *CandleStore
}

// Init will get DataManager ready to go.
func (dm *DataManager) Init() {
	if dm.Downloader == nil {
		dm.Downloader = &Downloader{
			Client: newHTTPClient(),
		}
	}
	if dm.Store == nil {
		dm.Store = &CandleStore{
			Basedir: "../../tmp/candles",
			Source:  "Dukascopy",
		}
	}
}

func (dm *DataManager) Sync(ctx context.Context) error {
	// 1. Build inventory
	inv, err := dm.BuildInventory(ctx)
	if err != nil {
		return fmt.Errorf("build inventory: %w", err)
	}

	// 2. Plan missing raw tick downloads
	plan, err := dm.Plan(ctx, inv)
	if err != nil {
		return fmt.Errorf("build plan: %w", err)
	}

	download := false
	if download {
		if err := dm.ExecuteDownloads(ctx, plan); err != nil {
			return fmt.Errorf("execute downloads: %w", err)
		}

		// 4. Refresh inventory after downloads
		inv, err = dm.BuildInventory(ctx)
		if err != nil {
			return fmt.Errorf("refresh inventory: %w", err)
		}
	}

	// 5. Plan/build M1 from available raw tick hours
	if err := dm.BuildM1(ctx, plan); err != nil {
		return fmt.Errorf("build M1: %w", err)
	}

	return nil
}

func (dm *DataManager) BuildInventory(ctx context.Context) (*Inventory, error) {
	b := NewInventoryBuilder(dm.DukasRoot, dm.Store.Basedir)

	inv, err := b.Build(ctx)
	if err != nil {
		return nil, err
	}
	return inv, nil
}

func (dm *DataManager) Plan(ctx context.Context, inv *Inventory) (plan *Plan, err error) {
	plan = &Plan{}

	start := types.FromTime(dm.Start)
	end := types.FromTime(dm.End)
	r := types.NewTimeRange(start, end)

	for _, sym := range dm.Instruments {
		var df *datafile
		for ts := r.Start; ts < r.End; ts += 3600 {
			t := time.Unix(int64(ts), 0).UTC()
			df = newDatafile(dm.DukasRoot, sym, t)

			if !df.Exists() {
				plan.downloads = append(plan.downloads, df)
				continue
			}

			if df.bytes <= 0 {
				// Check for the weekend
				continue
			}
			plan.buildM1 = append(plan.buildM1, df)
		}
		// fmt.Printf("the plan: %s downloads: %d - candles: %d\n", sym, len(plan.downloads), len(plan.buildM1))
	}
	slices.Reverse(plan.buildM1)
	return plan, nil
}

func (dm *DataManager) ExecuteDownloads(ctx context.Context, plan *Plan) error {
	if len(plan.downloads) == 0 {
		return nil
	}

	q := make(chan *datafile, 1024)
	dlWG := dm.Downloader.startDownloader(ctx, q)
	go func() {
		defer close(q)
		for _, df := range plan.downloads {
			select {
			case <-ctx.Done():
				return
			case q <- df:
			}
		}
	}()

	dlWG.Wait()
	return nil
}

func (dm *DataManager) BuildM1(ctx context.Context, plan *Plan) error {
	fmt.Println("BUILDM1")

	sort.Slice(plan.buildM1, func(i, j int) bool {
		a, b := plan.buildM1[i], plan.buildM1[j]

		if a.Instrument() != b.Instrument() {
			return a.Instrument() < b.Instrument()
		}
		return a.Time.Before(b.Time)
	})
	slices.Reverse(plan.buildM1)

	var cur *market.CandleSet
	hours := 0

	flush := func() error {
		if cur == nil {
			return nil
		}
		return dm.Store.WriteCSV(cur)
	}

	for _, df := range plan.buildM1 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		hourSet, err := df.buildM1(ctx)
		if err != nil {
			return fmt.Errorf("buildM1 failed for %s: %w", df.Path(), err)
		}
		if hourSet == nil {
			continue
		}
		hours++

		// TODO create an index for the instrument, time frame and range
		monthStart := market.FloorToMonthUTC(hourSet.Start)
		// Do a better job of ensuring we have not gotten out of order
		if cur == nil ||
			cur.Instrument.Name != hourSet.Instrument.Name ||
			cur.Start != monthStart {

			if err := flush(); err != nil {
				return err
			}

			cur, err = market.NewMonthlyCandleSet(
				hourSet.Instrument,
				hourSet.Timeframe,
				monthStart,
				hourSet.Scale,
				hourSet.Source,
			)
			if err != nil {
				return err
			}
		}

		if err := cur.Merge(hourSet); err != nil {
			return fmt.Errorf("merge hour set failed: %w", err)
		}
	}

	if err := flush(); err != nil {
		return err
	}

	fmt.Printf("Hours processed: %d\n", hours)
	return nil
}
