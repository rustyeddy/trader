package main

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	svc "github.com/rustyeddy/trader/service/marketdata"
)

func TestResolveFormatter_Table(t *testing.T) {
	f, err := resolveFormatter("table")
	require.NoError(t, err)
	require.IsType(t, tableFormatter{}, f)
}

func TestResolveFormatter_JSON(t *testing.T) {
	f, err := resolveFormatter("json")
	require.NoError(t, err)
	require.IsType(t, jsonFormatter{}, f)
}

func TestResolveFormatter_RejectsUnknown(t *testing.T) {
	_, err := resolveFormatter("xml")
	require.Error(t, err)
}

func sampleBar(t *testing.T) marketdata.Bar {
	t.Helper()
	return marketdata.Bar{
		Time:  time.Date(2024, time.January, 2, 22, 0, 0, 0, time.UTC),
		Open:  num.MustParsePrice("1.10000"),
		High:  num.MustParsePrice("1.10100"),
		Low:   num.MustParsePrice("1.09900"),
		Close: num.MustParsePrice("1.10050"),
	}
}

func TestTableFormatter_FormatBars(t *testing.T) {
	var buf bytes.Buffer
	err := tableFormatter{}.FormatBars(&buf, svc.BarsResponse{Bars: []marketdata.Bar{sampleBar(t)}})
	require.NoError(t, err)
	require.Contains(t, buf.String(), "2024-01-02T22:00:00Z")
	require.Contains(t, buf.String(), "O=1.1")
}

func TestJSONFormatter_FormatBars(t *testing.T) {
	var buf bytes.Buffer
	err := jsonFormatter{}.FormatBars(&buf, svc.BarsResponse{Bars: []marketdata.Bar{sampleBar(t)}})
	require.NoError(t, err)

	var decoded struct {
		Bars []struct {
			Time  time.Time `json:"time"`
			Open  string    `json:"open"`
			High  string    `json:"high"`
			Low   string    `json:"low"`
			Close string    `json:"close"`
		} `json:"bars"`
	}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &decoded))
	require.Len(t, decoded.Bars, 1)
	require.Equal(t, "1.1", decoded.Bars[0].Open)
	require.True(t, decoded.Bars[0].Time.Equal(sampleBar(t).Time))
}

func TestJSONFormatter_FormatPlan_EmptyActionsIsEmptyArray(t *testing.T) {
	var buf bytes.Buffer
	err := jsonFormatter{}.FormatPlan(&buf, svc.PlanResponse{})
	require.NoError(t, err)

	var decoded struct {
		Actions []any `json:"actions"`
	}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &decoded))
	require.NotNil(t, decoded.Actions, "an empty Plan must still encode actions as [], not null")
	require.Empty(t, decoded.Actions)
}

func TestJSONFormatter_FormatPlan_ActionFields(t *testing.T) {
	plan := marketdata.Plan{Actions: []marketdata.Action{
		{Kind: marketdata.ActionNormalizeCanonical, Interval: marketdata.H1, Year: 2024, Month: time.January, Reason: "missing"},
	}}
	var buf bytes.Buffer
	err := jsonFormatter{}.FormatPlan(&buf, svc.PlanResponse{Plan: plan})
	require.NoError(t, err)

	var decoded struct {
		Actions []struct {
			Kind     string `json:"kind"`
			Interval string `json:"interval"`
			Year     int    `json:"year"`
			Month    int    `json:"month"`
			Reason   string `json:"reason"`
		} `json:"actions"`
	}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &decoded))
	require.Len(t, decoded.Actions, 1)
	require.Equal(t, "normalize-canonical", decoded.Actions[0].Kind)
	require.Equal(t, "H1", decoded.Actions[0].Interval)
	require.Equal(t, 2024, decoded.Actions[0].Year)
	require.Equal(t, 1, decoded.Actions[0].Month)
	require.Equal(t, "missing", decoded.Actions[0].Reason)
}

func TestTableFormatter_FormatUpdate_AlreadyCurrent(t *testing.T) {
	var buf bytes.Buffer
	err := tableFormatter{}.FormatUpdate(&buf, svc.UpdateResponse{})
	require.NoError(t, err)
	require.Contains(t, buf.String(), "already current")
}

func TestJSONFormatter_FormatUpdate_AlreadyCurrent(t *testing.T) {
	var buf bytes.Buffer
	err := jsonFormatter{}.FormatUpdate(&buf, svc.UpdateResponse{})
	require.NoError(t, err)

	var decoded struct {
		AlreadyCurrent bool `json:"already_current"`
		SyncPerformed  bool `json:"sync_performed"`
	}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &decoded))
	require.True(t, decoded.AlreadyCurrent)
	require.False(t, decoded.SyncPerformed)
}

func TestTableFormatter_FormatUpdateProgress_NeverClaimsAlreadyCurrent(t *testing.T) {
	var buf bytes.Buffer
	err := tableFormatter{}.FormatUpdateProgress(&buf, svc.UpdateResponse{})
	require.NoError(t, err)
	require.NotContains(t, buf.String(), "already current")
}

func TestJSONFormatter_FormatUpdateProgress_NeverClaimsAlreadyCurrent(t *testing.T) {
	var buf bytes.Buffer
	err := jsonFormatter{}.FormatUpdateProgress(&buf, svc.UpdateResponse{})
	require.NoError(t, err)

	var decoded struct {
		AlreadyCurrent bool `json:"already_current"`
	}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &decoded))
	require.False(t, decoded.AlreadyCurrent)
}

func TestJSONFormatter_FormatSync(t *testing.T) {
	result := marketdata.SyncResult{
		Downloaded: []marketdata.DownloadResult{{
			Action:         marketdata.Action{Interval: marketdata.H1, Year: 2024, Month: time.January},
			RecordsWritten: 3,
		}},
		Skipped: []marketdata.SkippedAction{{
			Action: marketdata.Action{Kind: marketdata.ActionRepairRaw, Interval: marketdata.H1, Year: 2024, Month: time.January},
			Reason: "raw partition invalid",
		}},
	}
	var buf bytes.Buffer
	err := jsonFormatter{}.FormatSync(&buf, svc.SyncResponse{Result: result})
	require.NoError(t, err)

	var decoded struct {
		Downloaded []struct {
			RecordsWritten int `json:"records_written"`
		} `json:"downloaded"`
		Skipped []struct {
			Kind   string `json:"kind"`
			Reason string `json:"reason"`
		} `json:"skipped"`
	}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &decoded))
	require.Len(t, decoded.Downloaded, 1)
	require.Equal(t, 3, decoded.Downloaded[0].RecordsWritten)
	require.Len(t, decoded.Skipped, 1)
	require.Equal(t, "repair-raw", decoded.Skipped[0].Kind)
	require.Equal(t, "raw partition invalid", decoded.Skipped[0].Reason)
}

func TestJSONFormatter_FormatBuild(t *testing.T) {
	result := marketdata.BuildResult{
		Published: []marketdata.PublishResult{{
			Action:   marketdata.Action{Interval: marketdata.H1, Year: 2024, Month: time.January},
			BarCount: 216,
		}},
	}
	var buf bytes.Buffer
	err := jsonFormatter{}.FormatBuild(&buf, svc.BuildResponse{Result: result})
	require.NoError(t, err)

	var decoded struct {
		Published []struct {
			BarCount int `json:"bar_count"`
		} `json:"published"`
	}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &decoded))
	require.Len(t, decoded.Published, 1)
	require.Equal(t, 216, decoded.Published[0].BarCount)
}

func TestJSONFormatter_FormatCoverage(t *testing.T) {
	span, err := marketdata.NewTimeRange(
		time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2024, time.January, 2, 0, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	cov := marketdata.Coverage{
		Partitions: []marketdata.PartitionCoverage{{Year: 2024, Month: time.January, Status: marketdata.PartitionCoverageMissing}},
		Gaps:       []marketdata.Gap{{State: marketdata.IntervalStateMissing, Span: span}},
	}

	var buf bytes.Buffer
	err = jsonFormatter{}.FormatCoverage(&buf, svc.CoverageResponse{Coverage: cov})
	require.NoError(t, err)

	var decoded struct {
		Partitions []struct {
			Year   int    `json:"year"`
			Month  int    `json:"month"`
			Status string `json:"status"`
		} `json:"partitions"`
		Gaps []struct {
			State string    `json:"state"`
			Start time.Time `json:"start"`
			End   time.Time `json:"end"`
		} `json:"gaps"`
	}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &decoded))
	require.Len(t, decoded.Partitions, 1)
	require.Equal(t, 2024, decoded.Partitions[0].Year)
	require.Equal(t, 1, decoded.Partitions[0].Month)
	require.Len(t, decoded.Gaps, 1)
}
