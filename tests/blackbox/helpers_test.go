//go:build blackbox

package blackbox

import (
	"fmt"
	"strings"
)

func contains(s, sub string) bool { return strings.Contains(s, sub) }

func f64(x float64) string {
	// stable formatting, enough precision for FX ticks
	return fmt.Sprintf("%.6f", x)
}
