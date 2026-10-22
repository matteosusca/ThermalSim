//go:build !linux && !darwin

package transport

// PTYPort is a stub for non-POSIX platforms (e.g., Windows).
type PTYPort struct{}

// OpenPTY returns ErrPTYUnsupported on platforms without POSIX /dev/ptmx.
func OpenPTY(symlinkPath string) (*PTYPort, error) {
	return nil, ErrPTYUnsupported
}

func (p *PTYPort) SlavePath() string             { return "" }
func (p *PTYPort) DevicePath() string            { return "" }
func (p *PTYPort) Read(b []byte) (int, error)    { return 0, ErrPTYUnsupported }
func (p *PTYPort) Write(b []byte) (int, error)   { return 0, ErrPTYUnsupported }
func (p *PTYPort) Close() error                  { return nil }
