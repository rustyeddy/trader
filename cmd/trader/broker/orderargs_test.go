package broker

import (
	"testing"

	"github.com/stretchr/testify/require"

	runtimeorder "github.com/rustyeddy/trader/internal/order"
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
		want runtimeorder.Type
	}{
		{"market", runtimeorder.Market},
		{"limit", runtimeorder.Limit},
		{"stop", runtimeorder.Stop},
		{"stop-limit", runtimeorder.StopLimit},
		{"stop_limit", runtimeorder.StopLimit},
		{"STOPLIMIT", runtimeorder.StopLimit},
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
		want runtimeorder.TimeInForce
	}{
		{"gtc", runtimeorder.GTC},
		{"DAY", runtimeorder.DAY},
		{"ioc", runtimeorder.IOC},
		{"FOK", runtimeorder.FOK},
	}
	for _, c := range cases {
		got, err := parseTimeInForce(c.in)
		require.NoError(t, err, c.in)
		require.Equal(t, c.want, got, c.in)
	}

	_, err := parseTimeInForce("whenever")
	require.ErrorContains(t, err, "invalid --tif")
}
