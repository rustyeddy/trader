package oanda

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type Client struct {
	BaseURL string // e.g. https://api-fxpractice.oanda.com
	Token   string
	HTTP    *http.Client
}

func BaseURL(env string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(env)) {
	case "practice", "demo":
		return "https://api-fxpractice.oanda.com", nil
	case "live":
		// return "https://api-fxtrade.oanda.com", nil
		return "", errors.New("Not Live Trading Allowed")
	default:
		return "", fmt.Errorf("unknown OANDA env %q (want practice|live)", env)
	}
}

func (c *Client) Get(ctx context.Context, path string, opts map[string]string) (io.ReadCloser, error) {
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	u, err := url.Parse(c.BaseURL)
	if err != nil {
		return nil, err
	}
	u.Path = path

	q := u.Query()
	for k, v := range opts {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		return nil, fmt.Errorf("oanda pricing stream http %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return resp.Body, nil
}
