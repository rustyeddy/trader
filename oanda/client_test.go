package oanda

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClient(t *testing.T) {
	t.Run("practice mode", func(t *testing.T) {
		client := NewClient("test-token", true)
		assert.Equal(t, PracticeURL, client.baseURL)
		assert.Equal(t, "test-token", client.token)
		assert.NotNil(t, client.httpClient)
	})

	t.Run("live mode", func(t *testing.T) {
		client := NewClient("test-token", false)
		assert.Equal(t, LiveURL, client.baseURL)
		assert.Equal(t, "test-token", client.token)
		assert.NotNil(t, client.httpClient)
	})
}

func TestGetCandles_Success(t *testing.T) {
	// Create mock server
	mockResponse := candlesResponse{
		Instrument:  "EUR_USD",
		Granularity: "M5",
		Candles: []apiCandle{
			{
				Complete: true,
				Volume:   100,
				Time:     "2024-01-01T10:00:00.000000000Z",
				Mid: candleData{
					O: "1.0850",
					H: "1.0860",
					L: "1.0840",
					C: "1.0855",
				},
			},
			{
				Complete: true,
				Volume:   150,
				Time:     "2024-01-01T10:05:00.000000000Z",
				Mid: candleData{
					O: "1.0855",
					H: "1.0870",
					L: "1.0850",
					C: "1.0865",
				},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify authorization header
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))

		// Verify query parameters
		assert.Equal(t, "M", r.URL.Query().Get("price"))
		assert.Equal(t, "M5", r.URL.Query().Get("granularity"))
		assert.Equal(t, "100", r.URL.Query().Get("count"))

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(mockResponse)
	}))
	defer server.Close()

	// Create client with mock server URL
	client := &Client{
		baseURL:    server.URL,
		token:      "test-token",
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	// Execute request
	candles, err := client.GetCandles(context.Background(), CandlesRequest{
		Instrument:  "EUR_USD",
		Price:       MidPrice,
		Granularity: M5,
		Count:       100,
	})

	require.NoError(t, err)
	require.Len(t, candles, 2)

	// Verify first candle
	assert.Equal(t, 1.0850, candles[0].Open)
	assert.Equal(t, 1.0860, candles[0].High)
	assert.Equal(t, 1.0840, candles[0].Low)
	assert.Equal(t, 1.0855, candles[0].Close)
	assert.Equal(t, 100.0, candles[0].Volume)

	// Verify second candle
	assert.Equal(t, 1.0855, candles[1].Open)
	assert.Equal(t, 1.0870, candles[1].High)
	assert.Equal(t, 1.0850, candles[1].Low)
	assert.Equal(t, 1.0865, candles[1].Close)
	assert.Equal(t, 150.0, candles[1].Volume)
}

func TestGetCandles_IncompleteCandles(t *testing.T) {
	// Create mock server with incomplete candle
	mockResponse := candlesResponse{
		Instrument:  "EUR_USD",
		Granularity: "M5",
		Candles: []apiCandle{
			{
				Complete: true,
				Volume:   100,
				Time:     "2024-01-01T10:00:00.000000000Z",
				Mid: candleData{
					O: "1.0850",
					H: "1.0860",
					L: "1.0840",
					C: "1.0855",
				},
			},
			{
				Complete: false, // Incomplete candle should be skipped
				Volume:   50,
				Time:     "2024-01-01T10:05:00.000000000Z",
				Mid: candleData{
					O: "1.0855",
					H: "1.0860",
					L: "1.0850",
					C: "1.0858",
				},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(mockResponse)
	}))
	defer server.Close()

	client := &Client{
		baseURL:    server.URL,
		token:      "test-token",
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	candles, err := client.GetCandles(context.Background(), CandlesRequest{
		Instrument:  "EUR_USD",
		Granularity: M5,
		Count:       10,
	})

	require.NoError(t, err)
	require.Len(t, candles, 1, "incomplete candle should be skipped")
}

func TestGetCandles_BidAskPrice(t *testing.T) {
	// Create mock server with bid/ask data
	mockResponse := candlesResponse{
		Instrument:  "EUR_USD",
		Granularity: "M5",
		Candles: []apiCandle{
			{
				Complete: true,
				Volume:   100,
				Time:     "2024-01-01T10:00:00.000000000Z",
				Bid: candleData{
					O: "1.0849",
					H: "1.0859",
					L: "1.0839",
					C: "1.0854",
				},
				Ask: candleData{
					O: "1.0851",
					H: "1.0861",
					L: "1.0841",
					C: "1.0856",
				},
			},
		},
	}

	t.Run("bid price", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "B", r.URL.Query().Get("price"))
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(mockResponse)
		}))
		defer server.Close()

		client := &Client{
			baseURL:    server.URL,
			token:      "test-token",
			httpClient: &http.Client{Timeout: 5 * time.Second},
		}

		candles, err := client.GetCandles(context.Background(), CandlesRequest{
			Instrument:  "EUR_USD",
			Price:       BidPrice,
			Granularity: M5,
			Count:       10,
		})

		require.NoError(t, err)
		require.Len(t, candles, 1)
		assert.Equal(t, 1.0849, candles[0].Open)
		assert.Equal(t, 1.0859, candles[0].High)
		assert.Equal(t, 1.0839, candles[0].Low)
		assert.Equal(t, 1.0854, candles[0].Close)
	})

	t.Run("ask price", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "A", r.URL.Query().Get("price"))
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(mockResponse)
		}))
		defer server.Close()

		client := &Client{
			baseURL:    server.URL,
			token:      "test-token",
			httpClient: &http.Client{Timeout: 5 * time.Second},
		}

		candles, err := client.GetCandles(context.Background(), CandlesRequest{
			Instrument:  "EUR_USD",
			Price:       AskPrice,
			Granularity: M5,
			Count:       10,
		})

		require.NoError(t, err)
		require.Len(t, candles, 1)
		assert.Equal(t, 1.0851, candles[0].Open)
		assert.Equal(t, 1.0861, candles[0].High)
		assert.Equal(t, 1.0841, candles[0].Low)
		assert.Equal(t, 1.0856, candles[0].Close)
	})
}

func TestGetCandles_TimeRange(t *testing.T) {
	mockResponse := candlesResponse{
		Instrument:  "EUR_USD",
		Granularity: "H1",
		Candles:     []apiCandle{},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify time range parameters
		assert.NotEmpty(t, r.URL.Query().Get("from"))
		assert.NotEmpty(t, r.URL.Query().Get("to"))
		assert.Empty(t, r.URL.Query().Get("count"))

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(mockResponse)
	}))
	defer server.Close()

	client := &Client{
		baseURL:    server.URL,
		token:      "test-token",
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)

	_, err := client.GetCandles(context.Background(), CandlesRequest{
		Instrument:  "EUR_USD",
		Granularity: H1,
		From:        &from,
		To:          &to,
	})

	require.NoError(t, err)
}

func TestGetCandles_Errors(t *testing.T) {
	t.Run("missing instrument", func(t *testing.T) {
		client := NewClient("test-token", true)
		_, err := client.GetCandles(context.Background(), CandlesRequest{
			Count: 10,
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "instrument is required")
	})

	t.Run("count exceeds maximum", func(t *testing.T) {
		client := NewClient("test-token", true)
		_, err := client.GetCandles(context.Background(), CandlesRequest{
			Instrument: "EUR_USD",
			Count:      6000, // Exceeds 5000 limit
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot exceed 5000")
	})

	t.Run("API error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"errorMessage": "Invalid access token"}`))
		}))
		defer server.Close()

		client := &Client{
			baseURL:    server.URL,
			token:      "invalid-token",
			httpClient: &http.Client{Timeout: 5 * time.Second},
		}

		_, err := client.GetCandles(context.Background(), CandlesRequest{
			Instrument: "EUR_USD",
			Count:      10,
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "API error")
	})
}

func TestGetCandles_DefaultValues(t *testing.T) {
	mockResponse := candlesResponse{
		Instrument:  "EUR_USD",
		Granularity: "S5",
		Candles:     []apiCandle{},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify default values
		assert.Equal(t, "M", r.URL.Query().Get("price"), "default price should be mid")
		assert.Equal(t, "S5", r.URL.Query().Get("granularity"), "default granularity should be S5")

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(mockResponse)
	}))
	defer server.Close()

	client := &Client{
		baseURL:    server.URL,
		token:      "test-token",
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	// Request without specifying price and granularity
	_, err := client.GetCandles(context.Background(), CandlesRequest{
		Instrument: "EUR_USD",
		Count:      10,
	})

	require.NoError(t, err)
}
