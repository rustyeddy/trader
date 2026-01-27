package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestFetchCandles_NoCountWithTimeRange verifies that when using a time range,
// the count parameter is not set (they are mutually exclusive per Oanda API)
func TestFetchCandles_NoCountWithTimeRange(t *testing.T) {
	// Create a mock server to capture the request
	var capturedURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURL = r.URL.String()
		
		// Verify that Authorization header is present
		if r.Header.Get("Authorization") == "" {
			t.Error("Missing Authorization header")
		}

		// Return a valid response
		resp := candleResp{
			Instrument:  "EUR_USD",
			Granularity: "H1",
			Candles: []candle{
				{
					Complete: true,
					Time:     "2024-01-01T10:00:00.000000000Z",
					Volume:   100,
					Mid: &ohlc{
						O: "1.0850",
						H: "1.0860",
						L: "1.0840",
						C: "1.0855",
					},
				},
			},
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Set up test parameters
	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)

	// Call fetchCandles
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := fetchCandles(
		context.Background(),
		client,
		server.URL,
		"test-token",
		"EUR_USD",
		"H1",
		"M",
		from,
		to,
	)

	// Check for errors
	if err != nil {
		t.Fatalf("fetchCandles failed: %v", err)
	}

	// Verify we got a response
	if resp == nil {
		t.Fatal("Expected non-nil response")
	}

	// Parse the captured URL to check parameters
	if capturedURL == "" {
		t.Fatal("No URL was captured")
	}

	// Check that the URL contains expected parameters
	if !contains(capturedURL, "from=") {
		t.Error("URL should contain 'from' parameter")
	}
	if !contains(capturedURL, "to=") {
		t.Error("URL should contain 'to' parameter")
	}
	
	// Most importantly: verify that count is NOT set
	if contains(capturedURL, "count=") {
		t.Error("URL should NOT contain 'count' parameter when using time range (from/to). This causes 'Maximum value for count exceeded' error.")
	}

	// Verify other expected parameters
	if !contains(capturedURL, "granularity=H1") {
		t.Error("URL should contain granularity parameter")
	}
	if !contains(capturedURL, "price=M") {
		t.Error("URL should contain price parameter")
	}
}

// TestFetchCandles_ValidResponse verifies that a valid response can be decoded
func TestFetchCandles_ValidResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := candleResp{
			Instrument:  "EUR_USD",
			Granularity: "M5",
			Candles: []candle{
				{
					Complete: true,
					Time:     "2024-01-01T10:00:00.000000000Z",
					Volume:   100,
					Mid: &ohlc{
						O: "1.0850",
						H: "1.0860",
						L: "1.0840",
						C: "1.0855",
					},
				},
				{
					Complete: true,
					Time:     "2024-01-01T10:05:00.000000000Z",
					Volume:   150,
					Mid: &ohlc{
						O: "1.0855",
						H: "1.0870",
						L: "1.0850",
						C: "1.0865",
					},
				},
			},
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := fetchCandles(
		context.Background(),
		client,
		server.URL,
		"test-token",
		"EUR_USD",
		"M5",
		"M",
		from,
		to,
	)

	if err != nil {
		t.Fatalf("fetchCandles failed: %v", err)
	}

	if resp == nil {
		t.Fatal("Expected non-nil response")
	}

	if resp.Instrument != "EUR_USD" {
		t.Errorf("Expected instrument EUR_USD, got %s", resp.Instrument)
	}

	if len(resp.Candles) != 2 {
		t.Errorf("Expected 2 candles, got %d", len(resp.Candles))
	}
}

// TestFetchCandles_ErrorResponse verifies error handling
func TestFetchCandles_ErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"errorMessage":"Maximum value for 'count' exceeded"}`))
	}))
	defer server.Close()

	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)

	client := &http.Client{Timeout: 5 * time.Second}
	_, err := fetchCandles(
		context.Background(),
		client,
		server.URL,
		"test-token",
		"EUR_USD",
		"H1",
		"M",
		from,
		to,
	)

	if err == nil {
		t.Fatal("Expected error for 400 response")
	}

	if !contains(err.Error(), "400") {
		t.Errorf("Error should mention status code 400: %v", err)
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
