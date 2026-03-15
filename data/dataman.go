package data

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
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
	CandleRoot string

	*Downloader
	Store *Store
}

// Init will get DataManager ready to go.
func (dm *DataManager) Init() {
	if dm.Downloader == nil {
		dm.Downloader = NewDownloader(dm.DukasRoot)
	}
	if dm.Store == nil {
		dm.Store = &Store{
			Basedir: "../../tmp/candles",
		}
	}
}

func (dm *DataManager) Sync(ctx context.Context) error {

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
	log.Print("Downloading...")

	download := true
	if download {
		if err := dm.ExecuteDownloads(ctx, plan); err != nil {
			return fmt.Errorf("execute downloads: %w", err)
		}
	}

	// for _, k := range plan.Download {
	// 	ws.MarkDownload(k)
	// 	go func(key AssetKey) {
	// 		defer ws.ClearDownload(key)
	// 		_ = dm.downloadHour(ctx, key)
	// 	}(k)
	// }

	// for _, task := range plan.BuildM1 {
	// 	ws.MarkBuild(task.Target)
	// 	go func(bt BuildTask) {
	// 		defer ws.ClearBuild(bt.Target)
	// 		_ = dm.buildM1Task(ctx, bt)
	// 	}(task)
	// }

	log.Println("buildng M1...")

	// 5. Plan/build M1 from available raw tick hours
	if err := dm.BuildM1(ctx, plan); err != nil {
		return fmt.Errorf("build M1: %w", err)
	}

	return nil
}

func (dm *DataManager) BuildInventory(ctx context.Context) (*Inventory, error) {
	b := NewInventoryBuilder(dm.DukasRoot, dm.CandleRoot)

	inv, err := b.Build(ctx)
	if err != nil {
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
		return dm.Store.WriteCSV(cur)
	}

	for _, key := range plan.BuildM1 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		df := newDatafile(dm.DukasRoot, key.Target.Instrument, key.Target.Time())
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

func planMissingTickDownloads(sym string, r types.TimeRange, inv *Inventory, ws *WorkState) []Key {
	var out []Key

	for ts := r.Start; ts < r.End; ts += 3600 {
		t := time.Unix(int64(ts), 0).UTC()

		if IsForexMarketClosed(t) {
			continue
		}

		key := Key{
			Source:     "dukascopy",
			Instrument: sym,
			Kind:       KindTick,
			TF:         types.H1,
			Year:       t.Year(),
			Month:      int(t.Month()),
			Day:        t.Day(),
			Hour:       t.Hour(),
		}

		if ws.IsDownloadQueuedOrActive(key) {
			continue
		}

		asset, ok := inv.Get(key)
		ready := ok && asset.Exists && asset.Complete && asset.Size > 0
		if ready {
			continue
		}

		out = append(out, key)
	}

	return out
}

func (dm *DataManager) PlanM1Builds(
	ctx context.Context,
	sym string,
	r types.TimeRange,
	inv *Inventory,
	ws *WorkState,
) ([]BuildTask, error) {
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
			Kind:       KindTick,
			TF:         types.H1,
			Year:       day.Year(),
			Month:      int(day.Month()),
			Day:        day.Day(),
		}

		if ws.IsBuildQueuedOrActive(target) {
			continue
		}

		inputs, ok := requiredTickHoursForDay(sym, day, inv)
		if !ok {
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

func (dm *DataManager) consumeHourIntoM1(
	ctx context.Context,
	df *datafile,
	builder *DenseM1Builder,
	w *M1CSVWriter,
) error {
	return df.forEachTick(ctx, func(t Tick) error {
		candles, err := builder.Add(t)
		if err != nil {
			return err
		}
		for _, c := range candles {
			if err := w.Write(c); err != nil {
				return err
			}
		}
		return nil
	})
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
func (dm *DataManager) m1Path(key Key) string {
	return filepath.Join(
		dm.CandleRoot,
		key.Instrument,
		"M1",
		fmt.Sprintf("%04d", key.Year),
		fmt.Sprintf("%02d", key.Month),
		fmt.Sprintf("%02d.csv", key.Day),
	)
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
			Instrument: sym,
			Kind:       KindTick,
			TF:         types.H1,
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

// func (dm *DataManager) Plan(ctx context.Context, inv *Inventory) (*Plan, error) {
// 	plan := &Plan{}

// 	start := types.FromTime(dm.Start)
// 	end := types.FromTime(dm.End)
// 	r := types.NewTimeRange(start, end)

// 	var tickHoursReady []Key

// 	for _, sym := range dm.Instruments {
// 		for ts := r.Start; ts < r.End; ts += 3600 {
// 			t := time.Unix(int64(ts), 0).UTC()

// 			if IsForexMarketClosed(t) {
// 				continue
// 			}

// 			key := Key{
// 				Source:     "dukascopy",
// 				Instrument: sym,
// 				Kind:       KindTick,
// 				TF:         types.H1,
// 				Year:       t.Year(),
// 				Month:      int(t.Month()),
// 				Day:        t.Day(),
// 				Hour:       t.Hour(),
// 			}

// 			asset, ok := inv.Get(key)
// 			if !ok || !asset.Exists || !asset.Complete || asset.Size <= 0 {
// 				plan.Download = append(plan.Download, key)
// 				continue
// 			}
// 			tickHoursReady = append(tickHoursReady, key)
// 		}
// 	}

// 	plan.BuildM1 = GroupTickHoursIntoM1Builds(tickHoursReady, inv)
// 	return plan, nil
// }

func GroupTickHoursIntoM1Builds(hours []Key, inv *Inventory) []Key {
	out := make([]Key, 0, len(hours))
	out = append(out, hours...)
	return out
}

func (dm *DataManager) ExecuteDownloads(ctx context.Context, plan *Plan) error {
	if len(plan.Download) == 0 {
		return nil
	}

	q := make(chan Key, 1024)
	dlWG := dm.Downloader.startDownloader(ctx, q)
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

	dlWG.Wait()
	return nil
}

// func (dm *DataManager) BuildM1(ctx context.Context, plan *Plan) error {
// 	sort.Slice(plan.BuildM1, func(i, j int) bool {
// 		a, b := plan.BuildM1[i], plan.BuildM1[j]

// 		if a.Instrument != b.Instrument {
// 			return a.Instrument < b.Instrument
// 		}
// 		return a.before(b)
// 	})
// 	slices.Reverse(plan.BuildM1)

// 	var cur *market.CandleSet
// 	hours := 0

// 	flush := func() error {
// 		if cur == nil {
// 			return nil
// 		}
// 		return dm.Store.WriteCSV(cur)
// 	}

// 	for _, key := range plan.BuildM1 {
// 		select {
// 		case <-ctx.Done():
// 			return ctx.Err()
// 		default:
// 		}

// 		df := newDatafile(dm.DukasRoot, key.Instrument, key.Time())
// 		hourSet, err := df.buildM1(ctx)
// 		if err != nil {
// 			return fmt.Errorf("buildM1 failed for %s: %w", df.Path(), err)
// 		}
// 		if hourSet == nil {
// 			continue
// 		}
// 		hours++

// 		// TODO create an index for the instrument, time frame and range
// 		monthStart := market.FloorToMonthUTC(hourSet.Start)
// 		// Do a better job of ensuring we have not gotten out of order
// 		if cur == nil ||
// 			cur.Instrument.Name != hourSet.Instrument.Name ||
// 			cur.Start != monthStart {

// 			if err := flush(); err != nil {
// 				return err
// 			}

// 			cur, err = market.NewMonthlyCandleSet(
// 				hourSet.Instrument,
// 				hourSet.Timeframe,
// 				monthStart,
// 				hourSet.Scale,
// 				hourSet.Source,
// 			)
// 			if err != nil {
// 				return err
// 			}
// 		}

// 		if err := cur.Merge(hourSet); err != nil {
// 			return fmt.Errorf("merge hour set failed: %w", err)
// 		}
// 	}

// 	if err := flush(); err != nil {
// 		return err
// 	}

// 	fmt.Printf("Hours processed: %d\n", hours)
// 	return nil
// }

type DenseM1Builder struct {
	cur     market.Candle
	haveCur bool
}

func NewDenseM1Builder() *DenseM1Builder {
	return &DenseM1Builder{}
}

func floorToMinute(ts types.Timemilli) types.Timemilli {
	return ts - (ts % 60)
}

func midPrice(t Tick) types.Price {
	return types.Price((int64(t.Bid) + int64(t.Ask)) / 2)
}

func (b *DenseM1Builder) Add(t Tick) ([]market.Candle, error) {
	// minute := floorToMinute(t.Timemilli)
	price := midPrice(t)

	if !b.haveCur {
		b.cur = market.Candle{
			Open:  price,
			High:  price,
			Low:   price,
			Close: price,
		}
		b.haveCur = true
		return nil, nil
	}

	// if minute < b.cur.Timestamp {
	// 	return nil, fmt.Errorf("out-of-order tick")
	// }

	// if minute == b.cur.Timemilli {
	if price > b.cur.High {
		b.cur.High = price
	}
	if price < b.cur.Low {
		b.cur.Low = price
	}
	b.cur.Close = price
	return nil, nil
	// }

	out := []market.Candle{b.cur}

	b.cur = market.Candle{
		Open:  price,
		High:  price,
		Low:   price,
		Close: price,
	}

	return out, nil
}

func (b *DenseM1Builder) Flush() ([]market.Candle, error) {
	if !b.haveCur {
		return nil, nil
	}
	return []market.Candle{b.cur}, nil
}
