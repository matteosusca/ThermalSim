//go:build darwin

package transport

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

const (
	// macOS XNU ioctl constants for /dev/ptmx allocation (sys/ttycom.h)
	ioctlTIOCPTYUNLK  = 0x20007452 // _IO('t', 82)
	ioctlTIOCPTYGNAME = 0x40807453 // _IOC(IOC_OUT, 't', 83, 128)
	ioctlTIOCPTYGRANT = 0x20007454 // _IO('t', 84)
)

// PTYPort represents a virtual serial port pair created via macOS /dev/ptmx.
type PTYPort struct {
	master         *os.File
	slaveKeepAlive *os.File
	slavePath      string
	symlinkPath    string
}

// OpenPTY creates a new pseudo-terminal pair on macOS and optionally symlinks the slave to symlinkPath.
func OpenPTY(symlinkPath string) (*PTYPort, error) {
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to open /dev/ptmx: %w", err)
	}

	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), uintptr(ioctlTIOCPTYGRANT), 0); errno != 0 {
		master.Close()
		return nil, fmt.Errorf("failed to grant PTY slave: %w", errno)
	}

	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), uintptr(ioctlTIOCPTYUNLK), 0); errno != 0 {
		master.Close()
		return nil, fmt.Errorf("failed to unlock PTY slave: %w", errno)
	}

	var nameBuf [128]byte
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), uintptr(ioctlTIOCPTYGNAME), uintptr(unsafe.Pointer(&nameBuf[0]))); errno != 0 {
		master.Close()
		return nil, fmt.Errorf("failed to get PTY slave name: %w", errno)
	}

	nullIdx := bytes.IndexByte(nameBuf[:], 0)
	if nullIdx < 0 {
		nullIdx = len(nameBuf)
	}
	slavePath := string(nameBuf[:nullIdx])

	slaveKeepAlive, err := os.OpenFile(slavePath, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err == nil {
		setRawTerminal(slaveKeepAlive.Fd())
	}

	if symlinkPath != "" {
		_ = os.Remove(symlinkPath)
		_ = os.MkdirAll(filepath.Dir(symlinkPath), 0755)
		if err := os.Symlink(slavePath, symlinkPath); err != nil {
			if slaveKeepAlive != nil {
				slaveKeepAlive.Close()
			}
			master.Close()
			return nil, fmt.Errorf("failed to create symlink %s -> %s: %w", symlinkPath, slavePath, err)
		}
	}

	return &PTYPort{
		master:         master,
		slaveKeepAlive: slaveKeepAlive,
		slavePath:      slavePath,
		symlinkPath:    symlinkPath,
	}, nil
}

func (p *PTYPort) SlavePath() string {
	return p.slavePath
}

func (p *PTYPort) DevicePath() string {
	if p.symlinkPath != "" {
		return p.symlinkPath
	}
	return p.slavePath
}

func (p *PTYPort) Read(b []byte) (int, error) {
	return p.master.Read(b)
}

func (p *PTYPort) Write(b []byte) (int, error) {
	return p.master.Write(b)
}

func (p *PTYPort) Close() error {
	if p.slaveKeepAlive != nil {
		p.slaveKeepAlive.Close()
	}
	if p.symlinkPath != "" {
		_ = os.Remove(p.symlinkPath)
	}
	return p.master.Close()
}

func setRawTerminal(fd uintptr) {
	var termios syscall.Termios
	// TIOCGETA on Darwin
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TIOCGETA, uintptr(unsafe.Pointer(&termios))); errno != 0 {
		return
	}

	termios.Iflag &^= syscall.IGNBRK | syscall.BRKINT | syscall.PARMRK | syscall.ISTRIP | syscall.INLCR | syscall.IGNCR | syscall.ICRNL | syscall.IXON
	termios.Oflag &^= syscall.OPOST
	termios.Lflag &^= syscall.ECHO | syscall.ECHONL | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	termios.Cflag &^= syscall.CSIZE | syscall.PARENB
	termios.Cflag |= syscall.CS8

	termios.Cc[syscall.VMIN] = 1
	termios.Cc[syscall.VTIME] = 0

	_, _, _ = syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TIOCSETA, uintptr(unsafe.Pointer(&termios)))
}
