package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/rustyeddy/trader/data"
)

type TimeFrame string

const (
	Ticks = "ticks"
	M1    = "m1"
	H1    = "h1"
	D1    = "d1"
)

func (dm *DataManager) WhatsMissing(ctx context.Context, instruments []string, tf TimeFrame, r Range) []*CandleSet {

	return cs
}

func (dm *DataManager) getTicks(ctx context.Context, datafiles []*Datafiles)    {}
func (dm *DataManager) buildCandles(ctx context.Context, tf TimeFrame, r Range) {}
func (dm *DataManager) validate(ctx, tf TimeFrame, r Range)

func (dm *DataManager) startDownloader(ctx context.Context, downloadQ chan *CandleSet, wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		select {

		case cs := <-dowloadQ:
			dm.downloader(ctx, cs)

		case <-ctx.Done():
			return
		}
	}

}

func (dm *DataManager) startCandleMaker(ctx context.Context, candleQ chan *CandleSet, wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		select {

		case cs := <-candleQ:
			dm.candleMaker(ctx, cs)
			dm.storeQ <- cs

		case <-ctx.Done():
			return
		}
	}
	wg.Done()
}

func (dm *DataManager) startCandleStore(ctx context.Context, storeQ chan *CandleSet, wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		select {

		case cs := <-storeQ:
			store.
				dm.candleMaker(ctx, cs)

		case <-ctx.Done():
			return
		}
	}
	wg.Done()

}

func example(ctx context.Context) {
	dm := &data.DataManager{
		Instruments: []string{"EURUSD", "USDJPY"},
		Start:       "2025-02-01",
		End:         "2025-03-01",
	}

	downloadQ := make(chan *CandleSet)
	candleQ := make(chan *CandleSet)
	dm.storeQ := make(chan *CandleSet)

	var wg sync.WaitGroup
	wg.Add(3)
	go dm.startCandleStore(ctx, storeQ, &wg)
	go dm.startDownloader(ctx, downloadQ, &wg)
	go dm.startCandleMaker(ctx, candleQ, &wg)

	// What instruments are supported?
	instr := dm.Instruments
	fmt.Println("Instruments supported: ", instr)

	// What range are we interested in
	r := dm.Range("2025-02-01", "2025-03-01")

	// What source files (ticks) are we missing?
	for _, instr := range dm.Instruments {
		for _, tf := range []string{"ticks", "m1", "h1", "d1"} {
			missing := dm.WhatsMissing(instr, tf, r)
			fmt.Printf("What is missing ", missing.String())

			switch tf {
			case Ticks:
				dlq <- missing

			case M1, H1, D1:
				candleQ <- missing

			default:
				fmt.Printf("Unknown timeframe: %s\n", tf)
			}
		}
	}
	close(dlQ)
	close(candleQ)
	close(dm.storeQ)
	// What derived candles are missing?
	wg.Wait()
}

// CandleSets
func datamanager() {
	inst := dm.Instruments
	rng := dm.Range(inst)
	missing := dm.Missing(inst, rng, ticks) // m1, h1, d1
	cs = dm.BuildCandles(inst, tf, rng)     // m1, h1, d1
	datafiles := df.Download(instruments, rng)
	candles := df.Decode(datafile) // should this be in data files
	ok := store.Save(candles)
	candles, err := store.Read(path)
	ok, err := dm.Complete(instr, tf, rng)
}

// candleSet is an in memory candle store
func candleSet() {
	// one instrment, one timeframe, one contiguous dense range
	candles := cs.Candles
	valid := cs.Validate()

	candles.Start()
	candles.End()

	c := cs.Candles[i]
	cs.Aggregate(h1) // m1, h1
	cs.Gaps()
	cs.Stats()
	itr := cs.Iterator()
}

// raw.bi5 -> internal ticks / M1 candles
func datafile() {
	// .bi5 - represents one raw hourly datasource
	df.Decode(fname.bi5)
	ticks := df.GetTicks()
	ok := df.Validate() // times and values
	itr := df.Iterator()
	m1 := cs.BuildM1(df)
}

func candleStore() {
	path := store.Path(cs)
	store.Mkdirs(path)
	cs, err := store.Read(instr, tf, rng)
	err := store.Write(cs)
	available := store.Files()
}
