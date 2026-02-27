package main

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
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
	alldata data
)

func main() {

	basedir := "./tmp/"
	dukasdir := basedir + "dukas/"

	println(os.Args[0])
	if len(os.Args) < 2 {
		fmt.Printf("I don't know what to do, bye.")
		os.Exit(2)
	}

	switch os.Args[1] {
	case "candles":
		createCandles(dukasdir)

	case "download":
		downloadData(dukasdir)

	case "list":
		listData(basedir)

	default:
		fmt.Printf("Unknown command: ", os.Args[1])
	}
}

func downloadData(basedir string) {
	dsQ := make(chan *dataset)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		handleDataset(dsQ)
	}()

	for _, inst := range market.InstrumentList {
		ds := &dataset{instrument: inst}
		alldata.datasets[inst] = ds
		ds.planFiles(basedir)
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

	for ds := range dsQ {
		for i := range ds.datafiles {
			df := &ds.datafiles[i]
			if !df.rawFileExists() {
				dlQ <- df
			}
		}
	}
	close(dlQ)
	wg.Wait()
}

func handleDownloads(dlQ <-chan *datafile) {
	ctx := context.Background()

	for df := range dlQ {
		if err := df.download(ctx); err != nil {
			fmt.Printf("download failed %s -> %s: %v\n", df.URL(), df.Path(), err)
		}
		time.Sleep(time.Millisecond * 500)
	}
}

func listData(basedir string) {
}

func createCandles(basedir string) {
	symbols := make(map[string]*dataset)
	for _, inst := range market.InstrumentList {
		symbols[inst] = nil
	}

	var wg sync.WaitGroup
	err := filepath.WalkDir(basedir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			fmt.Printf("preventing walk into a directory: %v\n", err)
			return err
		}

		// 2. Process the file or directory
		if d.IsDir() {
			return nil
		}
		df := &datafile{}
		err = df.parsePath(path)
		if err != nil {
			return err
		}

		m1Q := make(chan Tick)
		go func() {
			wg.Add(1)
			m1maker(m1Q)
			defer wg.Done()
		}()

		// fmt.Println(df.Path())
		ctx, _ := context.WithCancel(context.TODO())
		err = df.ForEachTick(ctx, func(t Tick) error {
			m1Q <- t

			// fmt.Printf("ITs a tick %+v\n", t)
			return nil
		})
		// fmt.Printf("ticks: %d\n", ticks)

		// 3. Return nil to continue the traversal
		return nil
	})

	wg.Wait()
	if err != nil {
		log.Fatalf("error during directory traversal: %s", err)
	}
}

func m1maker(m1Q <-chan Tick) {

	for t := range m1Q {
		fmt.Printf("TICK: %+v\n", t)
	}

}

func panicErr(err error) {
	if err != nil {
		panic(err)
	}
}
