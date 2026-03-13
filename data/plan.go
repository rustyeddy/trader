package data

import "log"

type Plan struct {
	Download       []AssetKey
	BuildM1        []AssetKey
	BuildH1        []AssetKey
	BuildD1        []AssetKey
	TickHoursReady []AssetKey
}

func (p Plan) Log() {
	log.Println("Plan: ")
	log.Printf("\tDownloads: %d", len(p.Download))
	log.Printf("\t Build M1: %d", len(p.BuildM1))
	log.Printf("\t Build H1: %d", len(p.BuildH1))
	log.Printf("\t Build D1: %d", len(p.BuildD1))
	log.Printf("\tTickHours: %d", len(p.TickHoursReady))
}
