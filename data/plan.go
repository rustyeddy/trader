package data

import (
	"log"
)

type Plan struct {
	Download []Key
	BuildM1  []BuildTask
	BuildH1  []BuildTask
	BuildD1  []BuildTask

	BlockedM1 []BuildDecision
	BlockedH1 []BuildDecision
	BlockedD1 []BuildDecision
}

func (p Plan) Log() {
	log.Println("Plan: ")
	log.Printf("\tDownloads: %d", len(p.Download))
	log.Printf("\t Build M1: %d", len(p.BuildM1))
	log.Printf("\t Build H1: %d", len(p.BuildH1))
	log.Printf("\t Build D1: %d", len(p.BuildD1))
}

type BuildTask struct {
	Key
	// Range  types.TimeRange
	Inputs []Key
	Kind   BuildKind
}

type BuildKind string

const (
	BuildM1 BuildKind = "m1_from_ticks"
	BuildH1 BuildKind = "h1_from_m1"
	BuildD1 BuildKind = "d1_from_h1"
)

type WorkState struct {
	activeDownloads map[Key]struct{}
	activeBuilds    map[Key]struct{}
}

func NewWorkState() *WorkState {
	return &WorkState{
		activeDownloads: make(map[Key]struct{}),
		activeBuilds:    make(map[Key]struct{}),
	}
}

func (ws *WorkState) IsDownloadQueuedOrActive(k Key) bool {
	_, ok := ws.activeDownloads[k]
	return ok
}

func (ws *WorkState) IsBuildQueuedOrActive(k Key) bool {
	_, ok := ws.activeBuilds[k]
	return ok
}

func (ws *WorkState) MarkDownload(k Key) {
	ws.activeDownloads[k] = struct{}{}
}

func (ws *WorkState) MarkBuild(k Key) {
	ws.activeBuilds[k] = struct{}{}
}

func (ws *WorkState) ClearDownload(k Key) {
	delete(ws.activeDownloads, k)
}

func (ws *WorkState) ClearBuild(k Key) {
	delete(ws.activeBuilds, k)
}
