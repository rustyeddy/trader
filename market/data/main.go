package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rustyeddy/trader/market"
)

type job struct {
	url  string
	dst  string // .bi5 path
	flat string // .bin output path
}

var (
	data = make(map[string]*dataset)
)

func main() {
	dsQ := make(chan *dataset)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		handleDataset(dsQ)
	}()

	for _, inst := range market.InstrumentList {
		ds := &dataset{instrument: inst}
		data[inst] = ds
		ds.gatherFiles("./tmp")
		dsQ <- ds
	}
	close(dsQ)
	wg.Wait()
}

func handleDataset(dsQ <-chan *dataset) {
	dlQ := make(chan *datafile)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		handleDownloads(dlQ)
	}()

	exists := 0
	notexists := 0
	for ds := range dsQ {
		for i := range ds.datafiles {
			df := &ds.datafiles[i]
			if !df.rawFileExists() {
				notexists++
				dlQ <- df
			} else {
				exists++
			}
		}
	}
	fmt.Printf("\rexits/notexists: %d/%d\n", exists, notexists)
	close(dlQ)
	wg.Wait()
}

func handleDownloads(dlQ <-chan *datafile) {
	ctx := context.Background()

	for df := range dlQ {
		if err := df.download(ctx); err != nil {
			fmt.Printf("download failed %s -> %s: %v\n", df.URL(), df.Path(), err)
		}
		time.Sleep(time.Second)
	}
}
