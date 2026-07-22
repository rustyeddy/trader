package oanda

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ResolveToken returns explicit if non-empty, otherwise falls back to
// reading ~/.config/oanda/pat.txt. Returns "" if neither yields a token.
func ResolveToken(explicit string) string {
	if explicit != "" {
		return explicit
	}
	return readTokenFile()
}

// readTokenFile is the fallback used when no token is passed explicitly.
// Lives here so every consumer (CLI, REST, MCP) gets the same resolution
// behavior instead of maintaining their own copy.
func readTokenFile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(home, ".config", "oanda", "pat.txt"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// NewClient resolves token (explicit, else the ~/.config/oanda/pat.txt
// fallback via ResolveToken) and env (via BaseURL), returning a
// ready-to-use Client.
func NewClient(env, token string) (*Client, error) {
	tok := ResolveToken(token)
	if tok == "" {
		return nil, fmt.Errorf("oanda: no token (set OANDA_TOKEN, pass explicitly, or save to ~/.config/oanda/pat.txt)")
	}
	baseURL, err := BaseURL(env)
	if err != nil {
		return nil, err
	}
	return &Client{BaseURL: baseURL, Token: tok}, nil
}
