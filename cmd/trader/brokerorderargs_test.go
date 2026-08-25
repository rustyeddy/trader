package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/order"
)

func TestParseOrderSide(t *testing.T) {
	cases := []struct {
		in   string
		want order.Side
	}{
		{"buy", order.Buy},
		{"BUY", order.Buy},
		{" Sell ", order.Sell},
	}
	for _, c := range cases {
		got, err := parseOrderSide(c.in)
		require.NoError(t, err, c.in)
		require.Equal(t, c.want, got, c.in)
	}

	_, err := parseOrderSide("sideways")
	require.ErrorContains(t, err, "invalid --side")
}

func TestParseOrderType(t *testing.T) {
	cases := []struct {
		in   string
		want order.Type
	}{
		{"market", order.Market},
		{"limit", order.Limit},
		{"stop", order.Stop},
		{"stop-limit", order.StopLimit},
		{"stop_limit", order.StopLimit},
		{"STOPLIMIT", order.StopLimit},
	}
	for _, c := range cases {
		got, err := parseOrderType(c.in)
		require.NoError(t, err, c.in)
		require.Equal(t, c.want, got, c.in)
	}

	_, err := parseOrderType("trailing")
	require.ErrorContains(t, err, "invalid --type")
}

func TestParseTimeInForce(t *testing.T) {
	cases := []struct {
		in   string
		want order.TimeInForce
	}{
		{"gtc", order.GTC},
		{"DAY", order.DAY},
		{"ioc", order.IOC},
		{"FOK", order.FOK},
	}
	for _, c := range cases {
		got, err := parseTimeInForce(c.in)
		require.NoError(t, err, c.in)
		require.Equal(t, c.want, got, c.in)
	}

	_, err := parseTimeInForce("whenever")
	require.ErrorContains(t, err, "invalid --tif")
}
