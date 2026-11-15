package marketdata

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func goodSource() SourcePartition {
	return SourcePartition{
		Source: "oanda", Instrument: "EURUSD", Interval: "h1", Year: 2024, Month: time.January,
		Fingerprint: "sha256:abc",
		Rows:        []SourceRow{{Row: 1, Complete: true}, {Row: 2, Complete: true}, {Row: 5}},
	}
}

func TestSourcePartition_Validate(t *testing.T) {
	for name, tc := range map[string]struct {
		mutate func(*SourcePartition)
		ok     bool
	}{
		"valid":                     {func(*SourcePartition) {}, true},
		"no rows":                   {func(p *SourcePartition) { p.Rows = nil }, true},
		"no source":                 {func(p *SourcePartition) { p.Source = "" }, false},
		"no instrument":             {func(p *SourcePartition) { p.Instrument = "" }, false},
		"no interval":               {func(p *SourcePartition) { p.Interval = "" }, false},
		"month zero":                {func(p *SourcePartition) { p.Month = 0 }, false},
		"month thirteen":            {func(p *SourcePartition) { p.Month = 13 }, false},
		"no fingerprint":            {func(p *SourcePartition) { p.Fingerprint = "" }, false},
		"row zero":                  {func(p *SourcePartition) { p.Rows[0].Row = 0 }, false},
		"row repeated":              {func(p *SourcePartition) { p.Rows[1].Row = 1 }, false},
		"row descending":            {func(p *SourcePartition) { p.Rows[2].Row = 1 }, false},
		"rows may skip":             {func(p *SourcePartition) { p.Rows[2].Row = 9 }, true},
		"row names the partition":   {func(p *SourcePartition) { p.Rows[0].Bar.Instrument = "EURUSD" }, true},
		"row of another instrument": {func(p *SourcePartition) { p.Rows[1].Bar.Instrument = "GBPUSD" }, false},
		"unvalidated prices":        {func(p *SourcePartition) { p.Rows[0].Bar = Bar{} }, true},
	} {
		t.Run(name, func(t *testing.T) {
			p := goodSource()
			tc.mutate(&p)

			err := p.Validate()

			if tc.ok {
				assert.NoError(t, err)
				return
			}
			assert.ErrorIs(t, err, ErrInvalidSource)
		})
	}
}
