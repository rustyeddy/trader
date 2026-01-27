package oanda

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/rustyeddy/trader/market"
)

const (
	// PracticeURL is the URL for OANDA's practice/demo environment
	PracticeURL = "https://api-fxpractice.oanda.com"
	// LiveURL is the URL for OANDA's live trading environment
	LiveURL = "https://api-fxtrade.oanda.com"
)

// Granularity represents the time frame for candles
type Granularity string

const (
	S5  Granularity = "S5"  // 5 seconds
	S10 Granularity = "S10" // 10 seconds
	S15 Granularity = "S15" // 15 seconds
	S30 Granularity = "S30" // 30 seconds
	M1  Granularity = "M1"  // 1 minute
	M2  Granularity = "M2"  // 2 minutes
	M4  Granularity = "M4"  // 4 minutes
	M5  Granularity = "M5"  // 5 minutes
	M10 Granularity = "M10" // 10 minutes
	M15 Granularity = "M15" // 15 minutes
	M30 Granularity = "M30" // 30 minutes
	H1  Granularity = "H1"  // 1 hour
	H2  Granularity = "H2"  // 2 hours
	H3  Granularity = "H3"  // 3 hours
	H4  Granularity = "H4"  // 4 hours
	H6  Granularity = "H6"  // 6 hours
	H8  Granularity = "H8"  // 8 hours
	H12 Granularity = "H12" // 12 hours
	D   Granularity = "D"   // 1 day
	W   Granularity = "W"   // 1 week
	M   Granularity = "M"   // 1 month
)

// PriceComponent represents the price component for candles
type PriceComponent string

const (
	MidPrice PriceComponent = "M"  // Midpoint candles
	BidPrice PriceComponent = "B"  // Bid candles
	AskPrice PriceComponent = "A"  // Ask candles
	BidAsk   PriceComponent = "BA" // Bid and Ask candles
)

// Client represents an OANDA API client
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// NewClient creates a new OANDA API client
func NewClient(token string, practice bool) *Client {
	baseURL := LiveURL
	if practice {
		baseURL = PracticeURL
	}

	return &Client{
		baseURL: baseURL,
		token:   token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// CandlesRequest represents parameters for fetching historical candles
type CandlesRequest struct {
	Instrument  string         // Required: The instrument to fetch candles for (e.g., "EUR_USD")
	Price       PriceComponent // Price component (default: MidPrice)
	Granularity Granularity    // Candle granularity (default: S5)
	Count       int            // Number of candles (max 5000, mutually exclusive with From/To)
	From        *time.Time     // Start time (ISO 8601)
	To          *time.Time     // End time (ISO 8601)
	Smooth      bool           // Use previous candle's close as open
}

// candleData represents the OHLC data in the API response
type candleData struct {
	O string `json:"o"` // Open price
	H string `json:"h"` // High price
	L string `json:"l"` // Low price
	C string `json:"c"` // Close price
}

// apiCandle represents a single candle in the API response
type apiCandle struct {
	Complete bool       `json:"complete"`
	Volume   int        `json:"volume"`
	Time     string     `json:"time"`
	Mid      candleData `json:"mid,omitempty"`
	Bid      candleData `json:"bid,omitempty"`
	Ask      candleData `json:"ask,omitempty"`
}

// candlesResponse represents the API response for candles
type candlesResponse struct {
	Instrument  string      `json:"instrument"`
	Granularity string      `json:"granularity"`
	Candles     []apiCandle `json:"candles"`
}

// GetCandles fetches historical candles from OANDA
func (c *Client) GetCandles(ctx context.Context, req CandlesRequest) ([]market.Candle, error) {
	if req.Instrument == "" {
		return nil, fmt.Errorf("instrument is required")
	}

	// Build query parameters
	params := url.Values{}
	
	// Set price component (default to mid)
	if req.Price == "" {
		req.Price = MidPrice
	}
	params.Set("price", string(req.Price))

	// Set granularity (default to S5)
	if req.Granularity == "" {
		req.Granularity = S5
	}
	params.Set("granularity", string(req.Granularity))

	// Set count or time range
	if req.Count > 0 {
		if req.Count > 5000 {
			return nil, fmt.Errorf("count cannot exceed 5000")
		}
		params.Set("count", fmt.Sprintf("%d", req.Count))
	} else {
		if req.From != nil {
			params.Set("from", req.From.Format(time.RFC3339))
		}
		if req.To != nil {
			params.Set("to", req.To.Format(time.RFC3339))
		}
	}

	if req.Smooth {
		params.Set("smooth", "true")
	}

	// Build URL
	apiURL := fmt.Sprintf("%s/v3/instruments/%s/candles?%s", c.baseURL, req.Instrument, params.Encode())

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	// Set authorization header
	httpReq.Header.Set("Authorization", "Bearer "+c.token)

	// Execute request
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	// Parse response
	var apiResp candlesResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	// Convert to market.Candle
	candles := make([]market.Candle, 0, len(apiResp.Candles))
	for _, ac := range apiResp.Candles {
		// Skip incomplete candles
		if !ac.Complete {
			continue
		}

		// Parse time
		t, err := time.Parse(time.RFC3339, ac.Time)
		if err != nil {
			return nil, fmt.Errorf("parse time %s: %w", ac.Time, err)
		}

		// Use the appropriate price data based on request
		var priceData candleData
		switch req.Price {
		case BidPrice:
			priceData = ac.Bid
		case AskPrice:
			priceData = ac.Ask
		default: // MidPrice
			priceData = ac.Mid
		}

		// Parse OHLC values
		open, err := parseFloat(priceData.O)
		if err != nil {
			return nil, fmt.Errorf("parse open price: %w", err)
		}
		high, err := parseFloat(priceData.H)
		if err != nil {
			return nil, fmt.Errorf("parse high price: %w", err)
		}
		low, err := parseFloat(priceData.L)
		if err != nil {
			return nil, fmt.Errorf("parse low price: %w", err)
		}
		close, err := parseFloat(priceData.C)
		if err != nil {
			return nil, fmt.Errorf("parse close price: %w", err)
		}

		candles = append(candles, market.Candle{
			Open:   open,
			High:   high,
			Low:    low,
			Close:  close,
			Time:   t,
			Volume: float64(ac.Volume),
		})
	}

	return candles, nil
}

// parseFloat parses a string to float64
func parseFloat(s string) (float64, error) {
	var f float64
	_, err := fmt.Sscanf(s, "%f", &f)
	return f, err
}
