package backtest

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

func collectConfigPaths(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat config path %q: %w", path, err)
	}
	if !info.IsDir() {
		return []string{path}, nil
	}

	matches, err := filepath.Glob(filepath.Join(path, "*.yml"))
	if err != nil {
		return nil, fmt.Errorf("glob config path %q: %w", path, err)
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("config directory %q contains no *.yml files", path)
	}

	sort.Strings(matches)
	return matches, nil
}
