package trader

import (
	tlog "github.com/rustyeddy/trader/log"
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
	tlog.Info("plan summary",
		"downloads", len(p.Download),
		"build_m1", len(p.BuildM1),
		"build_h1", len(p.BuildH1),
		"build_d1", len(p.BuildD1),
	)
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
