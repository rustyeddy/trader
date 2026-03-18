package data

import (
	"context"
	"fmt"
	"log"
	"slices"
	"sort"
	"sync"
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
	Instruments []string
	*downloader

	// internal fields used by legacy test helpers
	start time.Time
	end   time.Time
	data  map[string]*dataset
}

// dataset holds per-instrument data summary used by buildDatasets.
type dataset struct {
	symbol    string
	datafiles int
}

// NewDataManager constructs a DataManager for the given instruments and time range.
func NewDataManager(instruments []string, start, end time.Time) *DataManager {
	return &DataManager{
		Start:       start,
		End:         end,
		start:       start,
		end:         end,
		Instruments: instruments,
		data:        make(map[string]*dataset),
	}
}

// buildDatasets populates dm.data with one entry per instrument.
func (dm *DataManager) buildDatasets(ctx context.Context) {
	dm.data = make(map[string]*dataset, len(dm.Instruments))
	duration := dm.end.Sub(dm.start)
	hours := int(duration.Hours()) + 1
	for _, inst := range dm.Instruments {
		dm.data[inst] = &dataset{symbol: inst, datafiles: hours}
	}
}

// download returns a channel that signals completion of download tasks.
// It is a stub; real implementation should schedule actual downloads.
func (dm *DataManager) download(ctx context.Context) <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

// Init will get DataManager ready to go.
func (dm *DataManager) Init() {
	if dm.downloader == nil {
		dm.downloader = NewDownloader()
	}
}

func (dm *DataManager) Sync(ctx context.Context, download, build bool) error {
	log.Print("Building inventory...")
	// 1. Build inventory
	inv, err := dm.BuildInventory(ctx)
	if err != nil {
		return fmt.Errorf("build inventory: %w", err)
	}

	log.Print("Planning...")
	ws := NewWorkState()
	plan, err := dm.Plan(ctx, inv, ws)
	if err != nil {
		return fmt.Errorf("build plan: %w", err)
	}

	plan.Log()
	var wg sync.WaitGroup

	if download {
		log.Print("Downloading...")
		wg.Add(1)
		defer wg.Done()
		if err := dm.ExecuteDownloads(ctx, plan); err != nil {
			return fmt.Errorf("execute downloads: %w", err)
		}
	}

	if build {
		log.Println("buildng M1...")
		wg.Add(1)
		defer wg.Done()

		// 5. Plan/build M1 from available raw tick hours
		if err := dm.BuildM1(ctx, plan); err != nil {
			log.Printf("build M1: %v", err)
		}
	}

	wg.Wait()
	return nil
}

func (dm *DataManager) BuildInventory(ctx context.Context) (*Inventory, error) {
	inv := NewInventory()
	if err := store.scanFiles(inv); err != nil {
		return nil, err
	}
	return inv, nil
}

func (dm *DataManager) Plan(ctx context.Context, inv *Inventory, ws *WorkState) (*Plan, error) {
	plan := &Plan{}
	start := types.FromTime(dm.Start)
	end := types.FromTime(dm.End)
	r := types.NewTimeRange(start, end)

	for _, sym := range dm.Instruments {
		downloadKeys := planMissingTickDownloads(sym, r, inv, ws)
		plan.Download = append(plan.Download, downloadKeys...)

		m1Tasks, err := dm.PlanM1Builds(ctx, sym, r, inv, ws)
		if err != nil {
			return nil, fmt.Errorf("plan m1 builds for %s: %w", sym, err)
		}
		plan.BuildM1 = append(plan.BuildM1, m1Tasks...)
	}

	return plan, nil
}

func (dm *DataManager) BuildM1(ctx context.Context, plan *Plan) error {
	sort.Slice(plan.BuildM1, func(i, j int) bool {
		a, b := plan.BuildM1[i], plan.BuildM1[j]

		if a.Target.Instrument != b.Target.Instrument {
			return a.Target.Instrument < b.Target.Instrument
		}
		return a.Target.before(b.Target)
	})
	slices.Reverse(plan.BuildM1)

	var cur *market.CandleSet
	hours := 0

	flush := func() error {
		if cur == nil {
			return nil
		}
		return store.WriteCSV(cur)
	}

	for _, key := range plan.BuildM1 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		df := newDatafile(key.Target.Instrument, key.Target.Time())
		hourSet, err := df.buildM1(ctx)
		if err != nil {
			return fmt.Errorf("buildM1 failed for %s: %w", store.PathForAsset(df.key), err)
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

func planMissingTickDownloads(sym string, r types.TimeRange, inv *Inventory, ws *WorkState) []Key {
	var out []Key

	for ts := r.Start; ts < r.End; ts += 3600 {
		t := time.Unix(int64(ts), 0).UTC()

		if IsForexMarketClosed(t) {
			continue
		}

		key := Key{
			Source:     "dukascopy",
			Instrument: normalizeInstrument(sym),
			Kind:       KindTick,
			TF:         types.Ticks,
			Year:       t.Year(),
			Month:      int(t.Month()),
			Day:        t.Day(),
			Hour:       t.Hour(),
		}

		if ws.IsDownloadQueuedOrActive(key) {
			continue
		}

		asset, ok := inv.Get(key)
		if ok && asset.Exists && asset.Complete && asset.Size > 0 {
			continue
		}
		out = append(out, key)
	}

	return out
}

func (dm *DataManager) PlanM1Builds(ctx context.Context, sym string, r types.TimeRange,
	inv *Inventory, ws *WorkState) ([]BuildTask, error) {

	var tasks []BuildTask

	for _, day := range eachUTCDateInRange(r) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		target := Key{
			Source:     "derived",
			Instrument: sym,
			Kind:       KindCandle,
			TF:         types.M1,
			Year:       day.Year(),
			Month:      int(day.Month()),
		}

		if ws.IsBuildQueuedOrActive(target) {
			continue
		}

		inputs, ok := requiredTickHoursForDay(sym, day, inv)
		if !ok {
			log.Printf("Incomplete day %s - %s", sym, day)
			continue // not fully buildable yet
		}

		if !m1TargetNeedsBuild(target, inputs, inv) {
			continue
		}

		dayStart := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC)
		dayEnd := dayStart.Add(24 * time.Hour)

		tasks = append(tasks, BuildTask{
			Target: target,
			Range: types.NewTimeRange(
				types.FromTime(dayStart),
				types.FromTime(dayEnd),
			),
			Inputs: inputs,
			Kind:   BuildKindM1FromTicks,
		})
	}

	return tasks, nil
}

func m1TargetNeedsBuild(target Key, inputs []Key, inv *Inventory) bool {
	targetAsset, ok := inv.Get(target)
	if !ok || !targetAsset.Exists || !targetAsset.Complete || targetAsset.Size <= 0 {
		return true
	}

	for _, in := range inputs {
		inAsset, ok := inv.Get(in)
		if !ok {
			return true
		}

		if inAsset.UpdatedAt.After(targetAsset.UpdatedAt) {
			return true
		}
	}

	return false
}

func eachUTCDateInRange(r types.TimeRange) []time.Time {
	start := time.Unix(int64(r.Start), 0).UTC()
	end := time.Unix(int64(r.End), 0).UTC()

	cur := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
	last := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, time.UTC)

	var out []time.Time
	for !cur.After(last) {
		out = append(out, cur)
		cur = cur.Add(24 * time.Hour)
	}

	return out
}

func requiredTickHoursForDay(sym string, day time.Time, inv *Inventory) ([]Key, bool) {
	var inputs []Key

	dayStart := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC)
	dayEnd := dayStart.Add(24 * time.Hour)

	for t := dayStart; t.Before(dayEnd); t = t.Add(time.Hour) {
		if IsForexMarketClosed(t) {
			continue
		}

		key := Key{
			Source:     "dukascopy",
			Instrument: normalizeInstrument(sym),
			Kind:       KindTick,
			TF:         types.Ticks,
			Year:       t.Year(),
			Month:      int(t.Month()),
			Day:        t.Day(),
			Hour:       t.Hour(),
		}

		asset, ok := inv.Get(key)
		ready := ok && asset.Exists && asset.Complete && asset.Size > 0
		if !ready {
			return nil, false
		}

		inputs = append(inputs, key)
	}

	return inputs, true
}

func (dm *DataManager) ExecuteDownloads(ctx context.Context, plan *Plan) error {
	if len(plan.Download) == 0 {
		return nil
	}

	q := make(chan Key, 1024)
	wg := dm.downloader.startDownloader(ctx, q)
	go func() {
		defer close(q)
		slices.Reverse(plan.Download)
		for _, key := range plan.Download {
			select {
			case <-ctx.Done():
				return
			case q <- key:
			}
		}
	}()

	wg.Wait()
	return nil
}
