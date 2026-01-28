package oanda

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDownloadCandlesToCSV_MissingInputs(t *testing.T) {
	t.Parallel()

	opts := CandlesOptions{Instrument: "EUR_USD", Granularity: "M1"}

	tests := []struct {
		name   string
		client Client
		opts   CandlesOptions
		want   string
	}{
		{
			name:   "missing token",
			client: Client{BaseURL: "http://example.com"},
			opts:   opts,
			want:   "missing token",
		},
		{
			name:   "missing base url",
			client: Client{Token: "t"},
			opts:   opts,
			want:   "missing base url",
		},
		{
			name:   "missing instrument",
			client: Client{Token: "t", BaseURL: "http://example.com"},
			opts:   CandlesOptions{Granularity: "M1"},
			want:   "missing instrument",
		},
		{
			name:   "missing granularity",
			client: Client{Token: "t", BaseURL: "http://example.com"},
			opts:   CandlesOptions{Instrument: "EUR_USD"},
			want:   "missing granularity",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			_, err := tt.client.DownloadCandlesToCSV(context.Background(), tt.opts, &buf)
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestDownloadCandlesToCSV_WritesCSV_DefaultPrice(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v3/instruments/EUR_USD/candles", r.URL.Path)
		require.Equal(t, "M1", r.URL.Query().Get("granularity"))
		require.Equal(t, "M", r.URL.Query().Get("price"))
		require.Equal(t, "2", r.URL.Query().Get("count"))
		require.Equal(t, "Bearer token", r.Header.Get("Authorization"))

		resp := map[string]any{
			"instrument":  "EUR_USD",
			"granularity": "M1",
			"candles": []map[string]any{
				{
					"complete": true,
					"time":     "2024-01-01T00:00:00Z",
					"volume":   10,
					"mid": map[string]string{
						"o": "1.1", "h": "1.2", "l": "1.0", "c": "1.15",
					},
				},
				{
					"complete": false,
					"time":     "2024-01-01T00:01:00Z",
					"volume":   5,
					"mid": map[string]string{
						"o": "1.15", "h": "1.16", "l": "1.14", "c": "1.15",
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	srv := httptest.NewServer(handler)
	defer srv.Close()

	client := Client{
		BaseURL: srv.URL,
		Token:   "token",
	}

	var buf bytes.Buffer
	written, err := client.DownloadCandlesToCSV(
		context.Background(),
		CandlesOptions{
			Instrument:  "EUR_USD",
			Granularity: "M1",
			Count:       2,
		},
		&buf,
	)
	require.NoError(t, err)
	require.Equal(t, 2, written)

	r := csv.NewReader(bytes.NewReader(buf.Bytes()))
	rows, err := r.ReadAll()
	require.NoError(t, err)
	require.Len(t, rows, 3)
	require.Equal(t, []string{"time", "instrument", "granularity", "complete", "volume", "o", "h", "l", "c"}, rows[0])
	require.Equal(t, []string{"2024-01-01T00:00:00Z", "EUR_USD", "M1", "true", "10", "1.1", "1.2", "1.0", "1.15"}, rows[1])
	require.Equal(t, []string{"2024-01-01T00:01:00Z", "EUR_USD", "M1", "false", "5", "1.15", "1.16", "1.14", "1.15"}, rows[2])
}

func TestDownloadCandlesToCSV_RejectsBA(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"instrument":  "EUR_USD",
			"granularity": "M1",
			"candles": []map[string]any{
				{
					"complete": true,
					"time":     time.Now().UTC().Format(time.RFC3339Nano),
					"volume":   1,
					"bid": map[string]string{
						"o": "1.1", "h": "1.2", "l": "1.0", "c": "1.15",
					},
					"ask": map[string]string{
						"o": "1.2", "h": "1.3", "l": "1.1", "c": "1.25",
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	srv := httptest.NewServer(handler)
	defer srv.Close()

	client := Client{
		BaseURL: srv.URL,
		Token:   "token",
	}

	var buf bytes.Buffer
	written, err := client.DownloadCandlesToCSV(
		context.Background(),
		CandlesOptions{
			Instrument:  "EUR_USD",
			Granularity: "M1",
			Price:       "BA",
			Count:       1,
		},
		&buf,
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "price=BA not supported")
	require.Equal(t, 0, written)
}
