package data

import (
	"log"

	"github.com/rustyeddy/trader/types"
)

type Plan struct {
	Download []AssetKey
	BuildM1  []BuildTask
	BuildH1  []BuildTask
	BuildD1  []BuildTask
}

func (p Plan) Log() {
	log.Println("Plan: ")
	log.Printf("\tDownloads: %d", len(p.Download))
	log.Printf("\t Build M1: %d", len(p.BuildM1))
	log.Printf("\t Build H1: %d", len(p.BuildH1))
	log.Printf("\t Build D1: %d", len(p.BuildD1))
}

type BuildTask struct {
	Target AssetKey
	Range  types.TimeRange
	Inputs []AssetKey
	Kind   BuildKind
}

type BuildKind string

const (
	BuildKindM1FromTicks BuildKind = "m1_from_ticks"
)

type WorkState struct {
	activeDownloads map[AssetKey]struct{}
	activeBuilds    map[AssetKey]struct{}
}

func NewWorkState() *WorkState {
	return &WorkState{
		activeDownloads: make(map[AssetKey]struct{}),
		activeBuilds:    make(map[AssetKey]struct{}),
	}
}

func (ws *WorkState) IsDownloadQueuedOrActive(k AssetKey) bool {
	_, ok := ws.activeDownloads[k]
	return ok
}

func (ws *WorkState) IsBuildQueuedOrActive(k AssetKey) bool {
	_, ok := ws.activeBuilds[k]
	return ok
}

func (ws *WorkState) MarkDownload(k AssetKey) {
	ws.activeDownloads[k] = struct{}{}
}

func (ws *WorkState) MarkBuild(k AssetKey) {
	ws.activeBuilds[k] = struct{}{}
}

func (ws *WorkState) ClearDownload(k AssetKey) {
	delete(ws.activeDownloads, k)
}

func (ws *WorkState) ClearBuild(k AssetKey) {
	delete(ws.activeBuilds, k)
}
