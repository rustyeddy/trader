package backtest

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// collectConfigPaths returns the config file(s) to run. If path points to a
// single file it is returned as-is. If it is a directory, all *.yml and
// *.json files are returned in sorted order.
func collectConfigPaths(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat config path %q: %w", path, err)
	}
	if !info.IsDir() {
		return []string{path}, nil
	}

	var matches []string
	for _, pattern := range []string{"*.yml", "*.json"} {
		m, err := filepath.Glob(filepath.Join(path, pattern))
		if err != nil {
			return nil, fmt.Errorf("glob %q in %q: %w", pattern, path, err)
		}
		matches = append(matches, m...)
	}

	if len(matches) == 0 {
		return nil, fmt.Errorf("config directory %q contains no .yml/.json files", path)
	}

	sort.Strings(matches)
	return matches, nil
}
