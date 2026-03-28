//go:build windows

package log

import "fmt"

// newSyslogWriter is not supported on Windows.
func newSyslogWriter(_ string) (*syslogWriter, error) {
	return nil, fmt.Errorf("syslog is not supported on Windows")
}

// syslogWriter is a placeholder type on Windows.
type syslogWriter struct{}

func (sw *syslogWriter) Write(p []byte) (int, error) {
	return 0, fmt.Errorf("syslog is not supported on Windows")
}
