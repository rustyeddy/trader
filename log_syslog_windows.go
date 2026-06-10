//go:build windows

package trader

import "fmt"

// newSyslogHandler is not supported on Windows.
func newSyslogHandler(_ string) (*syslogHandler, error) {
	return nil, fmt.Errorf("syslog is not supported on Windows")
}

// syslogHandler is a placeholder type on Windows.
type syslogHandler struct{}

func (sw *syslogHandler) Close() error {
	return fmt.Errorf("syslog is not supported on Windows")
}
