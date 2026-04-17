package data

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"sort"
	"time"

	tlog "github.com/rustyeddy/trader/log"
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

	inventory *Inventory
	wants     *Wantlist
	plan      *Plan
}

// NewDataManager constructs a DataManager for the given instruments and time range.
func NewDataManager(instruments []string, start, end time.Time) *DataManager {
	return &DataManager{
		Start:       start,
		End:         end,
		Instruments: instruments,
	}
}

// Init will get DataManager ready to go.
func (dm *DataManager) Init() {
	if dm.downloader == nil {
		dm.downloader = NewDownloader()
	}
}

func (dm *DataManager) Sync(ctx context.Context, download, build bool) error {
	var err error

	tlog.Info("Building inventory")
	dm.inventory, err = BuildInventory(ctx)
	if err != nil {
		return err
	}

	tlog.Info("Building wantlist")
	dm.wants, err = dm.BuildWantList(ctx)
	if err != nil {
		return err
	}

	tlog.Info("Planning")
	dm.plan, err = dm.Plan(ctx)
	if err != nil {
		return err
	}
	dm.plan.Log()

	if download {
		tlog.Info("Starting downloads")
		if err := dm.ExecuteDownloads(ctx); err != nil {
			return err
		}
	}

	if build {
		tlog.Info("Re-building inventory")
		dm.inventory, err = BuildInventory(ctx)
		if err != nil {
			return err
		}

		tlog.Info("Re-building wantlist")
		dm.wants, err = dm.BuildWantList(ctx)
		if err != nil {
			return err
		}

		tlog.Info("Re-planning wantlist")
		dm.plan, err = dm.Plan(ctx)
		if err != nil {
			return err
		}
		dm.plan.Log()

		tlog.Info("Making candles")
		if err := dm.candleMaker(ctx); err != nil {
			return err
		}
	}

	return nil
}

func BuildInventory(ctx context.Context) (*Inventory, error) {
	inv := NewInventory()
	if err := store.scanFiles(inv); err != nil {
		return nil, err
	}
	return inv, nil
}

func (dm *DataManager) BuildWantList(ctx context.Context) (*Wantlist, error) {
	w := NewWantlist()
	for _, sym := range dm.Instruments {
		for year := dm.End.Year(); year >= dm.Start.Year(); year-- {
			for month := 1; month <= 12; month++ {
				for _, tf := range []types.Timeframe{types.M1, types.H1, types.D1} {
					key := Key{sym, "candles", KindCandle, tf, year, month, 0, 0}
					if !dm.inventory.HasComplete(key) {
						want := Want{
							Key:        key,
							WantReason: WantMissing,
						}
						w.Put(want)
					}
				}

				// now find all the ticks we are going to want
				ndays := types.DaysInMonth(year, month-1)
				for day := 1; day <= ndays; day++ {
					for hour := 0; hour < 24; hour++ {
						t := time.Date(year, time.Month(month), day, hour, 0, 0, 0, time.UTC)
						if types.IsForexMarketClosed(t) {
							continue
						}
						select {
						case <-ctx.Done():
						default:
						}
						key := Key{sym, "dukascopy", KindTick, types.Ticks, year, month, day, hour}
						if !dm.inventory.HasComplete(key) {
							want := Want{
								Key:        key,
								WantReason: WantMissing,
							}
							w.Put(want)
						}
					}
				}
			}
		}
	}
	return w, nil
}

func (dm *DataManager) Plan(ctx context.Context) (plan *Plan, err error) {

	plan = &Plan{}

	// walk through the want list.  For all ticks on the want list enque on download list
	// For all candles on want list determine if the provider is complete and ready
	dm.wants.items.Range(func(k Key, w Want) bool {
		select {
		case <-ctx.Done():
			return false
		default:
		}

		switch k.Kind {
		case KindTick:
			// we could fire this off right now to get it started...
			plan.Download = append(plan.Download, k)

		case KindCandle:
			switch k.TF {
			case types.M1:
				// if it is M1 we need to determine if all the required ticks
				// are already part of inventory. Otherwise this candle will
				// have to continue to wait for all ticks to be ready
				// for the month involved, are all (or enough) tick files present
				ready, keys := dm.inventory.TicksComplete(k)
				if ready {
					bt := BuildTask{
						Key:    k,
						Inputs: keys,
						Kind:   BuildM1,
					}
					plan.BuildM1 = append(plan.BuildM1, bt)
				}

			case types.H1:
				// H1 is only  dependent on M1, if M1 is in inventory we can
				// build H1 straight away
				km1 := k
				km1.TF = types.M1
				found := dm.inventory.HasComplete(km1)
				if found {
					bt := BuildTask{
						Key:    k,
						Inputs: []Key{km1},
						Kind:   BuildH1,
					}
					plan.BuildH1 = append(plan.BuildH1, bt)
				}

			case types.D1:
				// D1 is only dependent on H1, if H1 is in inventory we can
				// build D1 straight away
				kh1 := k
				kh1.TF = types.H1
				found := dm.inventory.HasComplete(kh1)
				if found {
					bt := BuildTask{
						Key:    k,
						Inputs: []Key{kh1},
						Kind:   BuildD1,
					}
					plan.BuildD1 = append(plan.BuildD1, bt)
				}

			default:
				panic("bad timeframe ")

			}

		default:
			panic("unknown k.Kind " + string(k.Kind))
		}
		return true
	})

	return plan, err
}

func (dm *DataManager) candleMaker(ctx context.Context) error {
	for _, bt := range dm.plan.BuildM1 {
		if err := buildM1(ctx, bt.Key, bt.Inputs, dm.wants); err != nil {
			return err
		}
	}

	for _, bt := range dm.plan.BuildH1 {
		if err := buildH1(ctx, bt.Key, bt.Inputs, dm.wants); err != nil {
			return err
		}
	}

	for _, bt := range dm.plan.BuildD1 {
		if err := buildD1(ctx, bt.Key, bt.Inputs, dm.wants); err != nil {
			return err
		}
	}

	return nil
}

func buildM1(ctx context.Context, k Key, inputs []Key, wants *Wantlist) error {
	if k.Kind != KindCandle {
		return fmt.Errorf("buildM1 requires candle key, got kind=%v", k.Kind)
	}
	if k.TF != types.M1 {
		return fmt.Errorf("buildM1 wrong timeframe: %v", k.TF)
	}
	if k.Day != 0 || k.Hour != 0 {
		return fmt.Errorf("buildM1 requires monthly candle key, got day=%d hour=%d", k.Day, k.Hour)
	}
	if len(inputs) == 0 {
		return nil
	}

	sort.Slice(inputs, func(i, j int) bool {
		return inputs[i].before(inputs[j])
	})

	monthStart := time.Date(k.Year, time.Month(k.Month), 1, 0, 0, 0, 0, time.UTC)

	monthSet, err := types.NewMonthlyCandleSet(
		types.NormalizeInstrument(k.Instrument),
		types.M1,
		types.FromTime(monthStart),
		types.PriceScale,
		SourceCandles,
	)
	if err != nil {
		return fmt.Errorf("new monthly candleset for %v: %w", k, err)
	}

	for _, tickKey := range inputs {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if tickKey.Kind != KindTick || tickKey.TF != types.Ticks {
			return fmt.Errorf("buildM1 input must be hourly tick key, got %+v", tickKey)
		}

		it, err := store.OpenTickIterator(tickKey)
		if err != nil {
			return fmt.Errorf("open tick iterator %s: %w", store.PathForAsset(tickKey), err)
		}

		hourSet, err := buildHourM1FromTickIterator(ctx, tickKey, it)
		if err != nil {
			return fmt.Errorf("build hour M1 %s: %w", store.PathForAsset(tickKey), err)
		}
		if hourSet == nil {
			continue
		}

		if err := monthSet.Merge(hourSet); err != nil {
			return fmt.Errorf("merge hour %s into month %s: %w",
				store.PathForAsset(tickKey), store.PathForAsset(k), err)
		}
	}

	if err := store.WriteCSV(monthSet); err != nil {
		return fmt.Errorf("write monthly M1 %s: %w", store.PathForAsset(k), err)
	}

	wants.Delete(k)
	return nil
}

func buildHourM1FromTickIterator(ctx context.Context, key Key, it Iterator[Tick]) (_ *types.CandleSet, err error) {
	defer func() {
		if it != nil {
			closeErr := it.Close()
			if err == nil && closeErr != nil {
				err = closeErr
			}
		}
	}()

	if key.Kind != KindTick || key.TF != types.Ticks {
		return nil, fmt.Errorf("buildHourM1FromTickIterator requires tick key, got %+v", key)
	}

	hourStartTime := time.Date(
		key.Year,
		time.Month(key.Month),
		key.Day,
		key.Hour,
		0, 0, 0, time.UTC,
	)
	hourStartMS := types.TimeMilliFromTime(hourStartTime)

	const minutesPerHour = 60

	cs := &types.CandleSet{
		Instrument: types.NormalizeInstrument(key.Instrument),
		Start:      types.FromTime(hourStartTime),
		Timeframe:  types.M1,
		Scale:      types.PriceScale,
		Source:     SourceCandles,
		Candles:    make([]types.Candle, minutesPerHour),
		Valid:      make([]uint64, (minutesPerHour+63)/64),
	}

	var (
		curIdx        = -1
		cur           types.Candle
		spreadSum     int64
		havePrevClose bool
		prevClose     types.Price
	)

	finalize := func() error {
		if curIdx < 0 || cur.Ticks <= 0 {
			return nil
		}

		ticks := int64(cur.Ticks)
		cur.AvgSpread = types.Price((spreadSum + ticks/2) / ticks)

		cs.Candles[curIdx] = cur
		cs.SetValid(curIdx)

		prevClose = cur.Close
		havePrevClose = true
		return nil
	}

	fillFlat := func(idx int, px types.Price) {
		// Dense placeholder candle. Intentionally NOT marked valid.
		cs.Candles[idx] = types.Candle{
			Open:  px,
			High:  px,
			Low:   px,
			Close: px,
			Ticks: 0,
		}
	}

	for it.Next() {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		tick := it.Item()
		ts := tick.Timemilli
		if ts <= 0 {
			return nil, fmt.Errorf("bad tick timestamp: %d", tick.Timemilli)
		}

		minuteOpen := ts.FloorToMinute()
		idx := int((minuteOpen - hourStartMS) / types.MinuteInMS)
		if idx < 0 || idx >= minutesPerHour {
			return nil, fmt.Errorf(
				"tick outside hour window: minute=%d hourStart=%d idx=%d",
				minuteOpen, hourStartMS, idx,
			)
		}

		mid := tick.Mid()
		spread := tick.Spread()

		if curIdx == -1 {
			curIdx = idx
			cur = types.Candle{
				Open:      mid,
				High:      mid,
				Low:       mid,
				Close:     mid,
				Ticks:     1,
				MaxSpread: spread,
			}
			spreadSum = int64(spread)
			continue
		}

		if idx == curIdx {
			if mid > cur.High {
				cur.High = mid
			}
			if mid < cur.Low {
				cur.Low = mid
			}
			cur.Close = mid
			cur.Ticks++

			if spread > cur.MaxSpread {
				cur.MaxSpread = spread
			}
			spreadSum += int64(spread)
			continue
		}

		if idx < curIdx {
			return nil, fmt.Errorf("out-of-order tick minute: idx %d < curIdx %d", idx, curIdx)
		}

		if err := finalize(); err != nil {
			return nil, err
		}

		if havePrevClose {
			for m := curIdx + 1; m < idx; m++ {
				if !cs.IsValid(m) && cs.Candles[m].IsZero() {
					fillFlat(m, prevClose)
				}
			}
		}

		curIdx = idx
		cur = types.Candle{
			Open:      mid,
			High:      mid,
			Low:       mid,
			Close:     mid,
			Ticks:     1,
			MaxSpread: spread,
		}
		spreadSum = int64(spread)
	}

	if err := it.Err(); err != nil {
		return nil, err
	}

	if curIdx == -1 {
		return nil, nil
	}

	if err := finalize(); err != nil {
		return nil, err
	}

	if havePrevClose {
		for m := curIdx + 1; m < minutesPerHour; m++ {
			if !cs.IsValid(m) && cs.Candles[m].IsZero() {
				fillFlat(m, prevClose)
			}
		}
	}

	return cs, nil
}

func buildH1(ctx context.Context, k Key, inputs []Key, wants *Wantlist) (err error) {
	if k.TF != types.H1 {
		return fmt.Errorf("buildH1 wrong timeframe: %v", k.TF)
	}
	if len(inputs) != 1 {
		return fmt.Errorf("buildH1 expected 1 input, got %d", len(inputs))
	}

	km1 := inputs[0]
	if km1.TF != types.M1 {
		return fmt.Errorf("buildH1 expected M1 input, got %v", km1.TF)
	}

	cs, err := store.ReadCSV(km1)
	if err != nil {
		return err
	}

	h1, err := cs.Aggregate(types.H1) // or cs.Aggregate(k.TF)
	if err != nil {
		return err
	}

	if err := store.WriteCSV(h1); err != nil {
		return err
	}

	wants.Delete(k)
	return nil
}

func buildD1(ctx context.Context, k Key, inputs []Key, wants *Wantlist) error {
	if k.TF != types.D1 {
		return fmt.Errorf("buildD1 wrong timeframe: %v", k.TF)
	}
	if len(inputs) != 1 {
		return fmt.Errorf("buildD1 expected 1 input, got %d", len(inputs))
	}

	kh1 := inputs[0]
	if kh1.TF != types.H1 {
		return fmt.Errorf("buildD1 expected H1 input, got %v", kh1.TF)
	}

	cs, err := store.ReadCSV(kh1)
	if err != nil {
		return err
	}

	d1, err := cs.Aggregate(types.D1) // or cs.Aggregate(k.TF)
	if err != nil {
		return err
	}

	if err := store.WriteCSV(d1); err != nil {
		return err
	}

	wants.Delete(k)
	return nil
}

func (dm *DataManager) ExecuteDownloads(ctx context.Context) error {
	if len(dm.plan.Download) == 0 {
		return nil
	}

	q := make(chan Key, 1024)
	wg := dm.downloader.startDownloader(ctx, dm, q)
	slices.Reverse(dm.plan.Download)
	for _, key := range dm.plan.Download {

		if ok := store.IsUsableTickFile(key); ok {
			continue
		}
		select {
		case <-ctx.Done():
			return nil
		case q <- key:
		}
	}
	wg.Wait()
	return nil
}

type CandleRequest struct {
	Source     string
	Instrument string
	Range      types.TimeRange
	Strict     bool
}

func (cr CandleRequest) Key() Key {
	return Key{
		Instrument: cr.Instrument,
		Source:     "candles",
		Kind:       KindCandle,
		TF:         cr.Range.TF,
	}
}

func (dm *DataManager) Candles(ctx context.Context, req CandleRequest) (CandleIterator, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	inst := types.NormalizeInstrument(req.Instrument)
	if inst == "" {
		return nil, fmt.Errorf("blank instrument")
	}

	switch req.Range.TF {
	case types.M1, types.H1, types.D1:
	default:
		return nil, fmt.Errorf("unsupported candle timeframe: %v", req.Range.TF)
	}

	if !req.Range.Valid() {
		return nil, fmt.Errorf("invalid candle range: %s", req.Range)
	}

	source := normalizeSource(req.Source)
	if source == "" {
		source = SourceCandles
	}

	// months := types.MonthsInRange(req.Range)
	months := req.Range.MonthsInRange()
	iters := make([]CandleIterator, 0, len(months))

	for _, ym := range months {
		if err := ctx.Err(); err != nil {
			_ = closeCandleIterators(iters)
			return nil, err
		}

		key := Key{
			Instrument: inst,
			Source:     source,
			Kind:       KindCandle,
			TF:         req.Range.TF,
			Year:       ym.Year,
			Month:      ym.Month,
		}

		cs, err := dm.loadCandleSet(ctx, key)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) && !req.Strict {
				continue
			}
			_ = closeCandleIterators(iters)
			return nil, fmt.Errorf("load candles %v: %w", key, err)
		}

		iters = append(iters, NewCandleSetIterator(cs, req.Range))
	}

	return NewChainedCandleIterator(iters...), nil
}

func (dm *DataManager) loadCandleSet(ctx context.Context, key Key) (*types.CandleSet, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return store.ReadCSV(key)
}

func closeCandleIterators(iters []CandleIterator) error {
	var firstErr error
	for _, it := range iters {
		if it == nil {
			continue
		}
		if err := it.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
