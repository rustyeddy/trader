package sdk_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/sdk"
)

type baseOnly struct{}

func (baseOnly) Describe() sdk.Descriptor                     { return sdk.Descriptor{Name: "base"} }
func (baseOnly) Start(context.Context, sdk.Environment) error { return nil }

type barsOnly struct{ baseOnly }

func (barsOnly) OnBars(context.Context, sdk.BarsEvent, sdk.View) ([]sdk.DescribedIntent, []sdk.DescribedSignal, error) {
	return nil, nil, nil
}

type barOnly struct{ baseOnly }

func (barOnly) OnBar(context.Context, sdk.BarEvent, sdk.View) ([]sdk.DescribedIntent, []sdk.DescribedSignal, error) {
	return nil, nil, nil
}

type bothShapes struct {
	barOnly
	barsOnly
}

func (b bothShapes) Describe() sdk.Descriptor                           { return sdk.Descriptor{Name: "both"} }
func (b bothShapes) Start(ctx context.Context, e sdk.Environment) error { return nil }

// Compile-time proof that each consumer shape needs only its own
// callback, and that Strategy remains a compatible alias.
var (
	_ sdk.BarConsumer  = barOnly{}
	_ sdk.Strategy     = barOnly{}
	_ sdk.BarsConsumer = barsOnly{}
)

// The delivery-mode check runs before the connection is touched, so a
// nil conn is safe for these rejections.
func TestServeConn_RejectsUnservableConsumers(t *testing.T) {
	tests := []struct {
		name  string
		strat sdk.ConsumerBase
		want  string
	}{
		{"neither", baseOnly{}, "neither OnBar nor OnBars"},
		{"both", bothShapes{}, "both OnBar and OnBars"},
		{"bars without requirements", barsOnly{}, "at least one requirement"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := sdk.ServeConn(context.Background(), nil, tt.strat)
			require.ErrorContains(t, err, tt.want)
		})
	}
}
