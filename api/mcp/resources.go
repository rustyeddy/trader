package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// handleResourcesList returns available MCP resources.
func (s *Server) handleResourcesList() (any, *rpcError) {
	resources := []map[string]any{
		{
			"uri":         "backtest://results",
			"name":        "Backtest Results",
			"description": "Backtest report summaries (.org files) in the default output directory",
			"mimeType":    "text/plain",
		},
		{
			"uri":         "config://configs",
			"name":        "Backtest Configs",
			"description": "YAML backtest configuration files in testdata/configs/",
			"mimeType":    "text/yaml",
		},
	}
	return map[string]any{"resources": resources}, nil
}

// handleResourcesRead reads a resource by URI.
func (s *Server) handleResourcesRead(ctx context.Context, raw json.RawMessage) (any, *rpcError) {
	var p struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(raw, &p); err != nil || p.URI == "" {
		return nil, &rpcError{Code: errInvalidParams, Message: "uri is required"}
	}

	switch {
	case p.URI == "backtest://results" || strings.HasPrefix(p.URI, "backtest://results/"):
		return s.readBacktestResource(p.URI)
	case p.URI == "config://configs" || strings.HasPrefix(p.URI, "config://configs/"):
		return s.readConfigResource(p.URI)
	default:
		return nil, &rpcError{Code: errMethodNotFound, Message: fmt.Sprintf("unknown resource: %s", p.URI)}
	}
}

func (s *Server) readBacktestResource(uri string) (any, *rpcError) {
	const outDir = "../trading/backtests"

	// backtest://results → list all .org files
	if uri == "backtest://results" {
		matches, _ := filepath.Glob(filepath.Join(outDir, "*.org"))
		var names []string
		for _, m := range matches {
			names = append(names, filepath.Base(m))
		}
		text := strings.Join(names, "\n")
		if text == "" {
			text = "(no backtest reports found in " + outDir + ")"
		}
		return resourceContent(uri, "text/plain", text), nil
	}

	// backtest://results/<name> → read the specific .org file
	name := strings.TrimPrefix(uri, "backtest://results/")
	path := filepath.Join(outDir, name)
	if !strings.HasSuffix(path, ".org") {
		path += ".org"
	}
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return errContent(fmt.Sprintf("read %s: %v", path, err)), nil
	}
	return resourceContent(uri, "text/plain", string(data)), nil
}

func (s *Server) readConfigResource(uri string) (any, *rpcError) {
	const cfgDir = "testdata/configs"

	if uri == "config://configs" {
		matches, _ := filepath.Glob(filepath.Join(cfgDir, "*.yaml"))
		matches2, _ := filepath.Glob(filepath.Join(cfgDir, "*.yml"))
		matches = append(matches, matches2...)
		var names []string
		for _, m := range matches {
			names = append(names, filepath.Base(m))
		}
		text := strings.Join(names, "\n")
		if text == "" {
			text = "(no config files found in " + cfgDir + ")"
		}
		return resourceContent(uri, "text/plain", text), nil
	}

	name := strings.TrimPrefix(uri, "config://configs/")
	path := filepath.Join(cfgDir, name)
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return errContent(fmt.Sprintf("read %s: %v", path, err)), nil
	}
	return resourceContent(uri, "text/yaml", string(data)), nil
}

func resourceContent(uri, mimeType, text string) map[string]any {
	return map[string]any{
		"contents": []map[string]any{
			{"uri": uri, "mimeType": mimeType, "text": text},
		},
	}
}
