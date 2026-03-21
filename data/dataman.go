package data

import (
	"context"
	"fmt"
	"log"
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
	Instruments []string
	*downloader
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

func (dm *DataManager) Sync(ctx context.Context, download, build bool) (err error) {
	log.Print("Building inventory...")

	// 1. Build inventory
	inv, err = BuildInventory(ctx)
	if err != nil {
		return fmt.Errorf("build inventory: %w", err)
	}
	log.Printf("inventory: %d", len(inv.items.m))

	wants, err = BuildWantList(ctx, dm.Instruments)
	if err != nil {
		return fmt.Errorf("build wantlist: %w", err)
	}
	log.Printf("wants: %d", wants.Len())

	log.Print("Start planning...")
	plan, err := dm.Plan(ctx, inv, wants)
	plan.Log()

	if download {
		log.Print("Downloading missing ticks...")
		if err := dm.ExecuteDownloads(ctx, plan); err != nil {
			log.Printf("execute downloads: %w", err)
			return err
		}
	}

	if build {
		log.Println("building candles...")
		if err := candleMaker(ctx, plan); err != nil {
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

func BuildWantList(ctx context.Context, symbols []string) (*Wantlist, error) {
	w := NewWantlist()
	for _, sym := range symbols {
		for year := 2025; year > 2003; year-- {
			for month := 1; month <= 12; month++ {
				for _, tf := range []types.Timeframe{types.M1, types.H1, types.D1} {
					key := Key{sym, "candles", KindCandle, tf, year, month, 0, 0}
					_, found := inv.Get(key)
					if !found {
						want := Want{key}
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
						_, found := inv.Get(key)
						if !found {
							want := Want{key}
							w.Put(want)
						}
					}
				}
			}
		}
	}
	return w, nil
}

func (dm *DataManager) Plan(ctx context.Context, inv *Inventory, wants *Wantlist) (plan *Plan, err error) {

	plan = &Plan{}

	// walk through the want list.  For all ticks on the want list enque on download list
	// For all candles on want list determine if the provider is complete and ready
	wants.items.Range(func(k Key, w Want) bool {
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
				ready, keys := inv.TicksComplete(k)
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
				_, found := inv.Get(km1)
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
				_, found := inv.Get(kh1)
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

func candleMaker(ctx context.Context, plan *Plan) error {
	for _, bt := range plan.BuildM1 {
		if err := buildM1(ctx, bt.Key, bt.Inputs); err != nil {
			return err
		}
	}

	for _, bt := range plan.BuildH1 {
		if err := buildH1(ctx, bt.Key, bt.Inputs); err != nil {
			return err
		}
	}

	for _, bt := range plan.BuildD1 {
		if err := buildD1(ctx, bt.Key, bt.Inputs); err != nil {
			return err
		}
	}

	return nil
}

func buildM1(ctx context.Context, k Key, inputs []Key) error {
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

	monthSet, err := market.NewMonthlyCandleSet(
		market.NormalizeInstrument(k.Instrument),
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

func buildHourM1FromTickIterator(ctx context.Context, key Key, it Iterator[Tick]) (_ *market.CandleSet, err error) {
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

	cs := &market.CandleSet{
		Instrument: market.NormalizeInstrument(key.Instrument),
		Start:      types.FromTime(hourStartTime),
		Timeframe:  types.M1,
		Scale:      types.PriceScale,
		Source:     SourceCandles,
		Candles:    make([]market.Candle, minutesPerHour),
		Valid:      make([]uint64, (minutesPerHour+63)/64),
	}

	var (
		curIdx        = -1
		cur           market.Candle
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
		cs.Candles[idx] = market.Candle{
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
			cur = market.Candle{
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
		cur = market.Candle{
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

func buildH1(ctx context.Context, k Key, inputs []Key) (err error) {
	if k.TF != types.H1 {
		return fmt.Errorf("buildH1 wrong timeframe: %v\n", k.TF)
	}

	if len(inputs) > 1 {
		return fmt.Errorf("buildH1 inputs are > 1: %d", len(inputs))
	}

	km1 := k
	km1.TF = types.M1
	cs, err := store.ReadCSV(km1)
	if err != nil {
		return err
	}
	h1, err := cs.Aggregate(k.TF)
	if err != nil {
		return err
	}

	// we were successful, now write to file and remove from want list
	err = store.WriteCSV(h1)
	if err != nil {
		return err
	}

	wants.Delete(k)
	return err
}

func buildD1(ctx context.Context, k Key, inputs []Key) (err error) {
	if k.TF != types.D1 {
		return fmt.Errorf("buildD1 wrong timeframe: %v\n", k.TF)
	}

	if len(inputs) > 1 {
		return fmt.Errorf("buildD1 inputs are > 1: %d", len(inputs))
	}

	kh1 := k
	kh1.TF = types.H1
	cs, err := store.ReadCSV(kh1)
	if err != nil {
		return err
	}
	d1, err := cs.Aggregate(kh1.TF)
	if err != nil {
		return err
	}

	// we were successful, now write to file and remove from want list
	err = store.WriteCSV(d1)
	if err != nil {
		return err
	}

	wants.Delete(k)

	return err
}

func (dm *DataManager) BuildM11(ctx context.Context, plan *Plan) error {
	sort.Slice(plan.BuildM1, func(i, j int) bool {
		a, b := plan.BuildM1[i], plan.BuildM1[j]

		if a.Key.Instrument != b.Key.Instrument {
			return a.Key.Instrument < b.Key.Instrument
		}
		return a.Key.before(b.Key)
	})

	hours := 0
	for _, task := range plan.BuildM1 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if len(task.Inputs) < 1 {
			continue
		}

		sort.Slice(task.Inputs, func(i, j int) bool {
			return task.Inputs[i].before(task.Inputs[j])
		})

		monthStart := time.Date(task.Key.Year, time.Month(task.Key.Month), 1, 0, 0, 0, 0, time.UTC)
		cur, err := market.NewMonthlyCandleSet(
			market.NormalizeInstrument(task.Key.Instrument),
			types.M1,
			types.FromTime(monthStart),
			types.PriceScale, // keep your current candle price scale expectation
			"candles",
		)
		if err != nil {
			return fmt.Errorf("new monthly candle set for %v: %w", task.Key, err)
		}

		for _, k := range task.Inputs {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			it, err := store.OpenTickIterator(k)
			if err != nil {
				return fmt.Errorf("Failed to get tick iterator %w", err)
			}

			hourSet, err := buildHourM1FromTickIterator(ctx, k, it)
			if err != nil {
				return fmt.Errorf("buildM1 failed for %s: %w", store.PathForAsset(k), err)
			}
			if hourSet == nil {
				continue
			}
			if err := cur.Merge(hourSet); err != nil {
				return fmt.Errorf("merge hour set into month %v failed: %w", task.Key, err)
			}
			hours++
		}

		if err := store.WriteCSV(cur); err != nil {
			return fmt.Errorf("write monthly M1 csv for %v: %w", task.Key, err)
		}
	}

	fmt.Printf("Hours processed: %d\n", hours)
	return nil
}

func planMissingTickDownloads(sym string, r types.TimeRange, inv *Inventory, ws *WorkState) []Key {
	var out []Key

	for ts := r.Start; ts < r.End; ts += 3600 {
		t := time.Unix(int64(ts), 0).UTC()

		if types.IsForexMarketClosed(t) {
			continue
		}

		key := Key{
			Source:     "dukascopy",
			Instrument: market.NormalizeInstrument(sym),
			Kind:       KindTick,
			TF:         types.Ticks,
			Year:       t.Year(),
			Month:      int(t.Month()),
			Day:        t.Day(),
			Hour:       t.Hour(),
		}

		if ok := store.IsUsableTickFile(key); ok {
			continue
		}

		if ws.IsDownloadQueuedOrActive(key) {
			continue
		}
		out = append(out, key)
	}

	return out
}

func m1KeyNeedsBuild(target Key, inputs []Key, inv *Inventory) bool {
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

// Move this to the Range type
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
		if types.IsForexMarketClosed(t) {
			continue
		}

		key := Key{
			Source:     "dukascopy",
			Instrument: market.NormalizeInstrument(sym),
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
	slices.Reverse(plan.Download)
	for _, key := range plan.Download {

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
	Timeframe  types.Timeframe
	Range      types.TimeRange
	Strict     bool
}

func (dm *DataManager) Candles(ctx context.Context, req CandleRequest) (Iterator[market.Candle], error) {
	// return an Iterator the CandleSet(s) that include the given range

	return nil, nil
}

type miss struct {
	candles map[types.Timeframe]Key
	ticks   []Key // missing ticks
}

type missKey struct {
	sym   string
	year  int
	month int
}

func examine(ctx context.Context, inv *Inventory) {
	missing := make(map[missKey]*miss)
	symbols := []string{"EURUSD", "GBPUSD", "USDJPY", "USDCHF"}
	var buildCandles []Key

	for _, sym := range symbols {
		for year := 2025; year > 2003; year-- {
			for month := 1; month <= 12; month++ {

				mkey := missKey{sym, year, month}
				m := &miss{
					candles: make(map[types.Timeframe]Key),
				}
				for _, tf := range []types.Timeframe{types.M1, types.H1, types.D1} {
					key := Key{sym, "candles", KindCandle, tf, year, month, 0, 0}
					asset, ok := inv.Get(key)
					if !ok || !asset.Exists || !asset.Complete {
						m.candles[tf] = key
					}
				}

				ndays := types.DaysInMonth(year, month-1)
				for day := 1; day <= ndays; day++ {
					for hour := 0; hour < 24; hour++ {
						select {
						case <-ctx.Done():
						default:
						}
						key := Key{sym, "dukascopy", KindTick, types.Ticks, year, month, day, hour}
						asset, ok := inv.Get(key)
						if !ok || !asset.Exists || !asset.Complete {

							t := time.Date(year, time.Month(month), day, hour, 0, 0, 0, time.UTC)
							if types.IsForexMarketClosed(t) {
								continue
							}

							// TODO check to make sure this is NOT a weekend or holiday before
							// adding to the missing ticks
							m.ticks = append(m.ticks, key)
						}
					}
				}

				if len(m.candles) > 0 || len(m.ticks) > 0 {
					missing[mkey] = m
				}

				if len(m.ticks) == 0 {
					for _, tf := range []types.Timeframe{types.M1, types.H1, types.D1} {
						if candles, ok := m.candles[tf]; ok {
							buildCandles = append(buildCandles, candles)
							break
						}
					}
				}
			}
		}
	}
	for _, k := range buildCandles {
		fmt.Println(k.Path())
	}
}
