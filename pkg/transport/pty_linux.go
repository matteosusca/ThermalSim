package transport

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

const (
	// Linux ioctl constants for PTY allocation
	ioctlTIOCGPTN   = 0x80045430 // Get PTY number
	ioctlTIOCSPTLCK = 0x40045431 // Lock/unlock PTY
)

// PTYPort represents a virtual serial port pair created via Linux /dev/ptmx.
type PTYPort struct {
	master         *os.File
	slaveKeepAlive *os.File
	slavePath      string
	symlinkPath    string
}

// OpenPTY creates a new pseudo-terminal pair and optionally symlinks the slave to symlinkPath.
func OpenPTY(symlinkPath string) (*PTYPort, error) {
	// 1. Open master /dev/ptmx
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to open /dev/ptmx: %w", err)
	}

	// 2. Unlock slave (TIOCSPTLCK with 0)
	var unlock int32 = 0
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), uintptr(ioctlTIOCSPTLCK), uintptr(unsafe.Pointer(&unlock))); errno != 0 {
		master.Close()
		return nil, fmt.Errorf("failed to unlock PTY slave: %w", errno)
	}

	// 3. Get slave PTS number (TIOCGPTN)
	var ptyNum uint32
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), uintptr(ioctlTIOCGPTN), uintptr(unsafe.Pointer(&ptyNum))); errno != 0 {
		master.Close()
		return nil, fmt.Errorf("failed to get PTY number: %w", errno)
	}

	slavePath := fmt.Sprintf("/dev/pts/%d", ptyNum)

	// 4. Configure slave PTS to raw mode and keep handle open to prevent EIO on client disconnect
	slaveKeepAlive, err := os.OpenFile(slavePath, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err == nil {
		setRawTerminal(slaveKeepAlive.Fd())
	}

	// 5. Create symlink if requested
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

// SlavePath returns the active /dev/pts/N path.
func (p *PTYPort) SlavePath() string {
	return p.slavePath
}

// DevicePath returns the symlink path if set, or the slave path.
func (p *PTYPort) DevicePath() string {
	if p.symlinkPath != "" {
		return p.symlinkPath
	}
	return p.slavePath
}

// Read reads raw bytes sent by the host software over the virtual serial link.
func (p *PTYPort) Read(b []byte) (int, error) {
	return p.master.Read(b)
}

// Write writes bytes back to the host software (for status queries like ESC v).
func (p *PTYPort) Write(b []byte) (int, error) {
	return p.master.Write(b)
}

// Close closes the PTY master, releases keep-alive, and removes the symlink.
func (p *PTYPort) Close() error {
	if p.slaveKeepAlive != nil {
		p.slaveKeepAlive.Close()
	}
	if p.symlinkPath != "" {
		_ = os.Remove(p.symlinkPath)
	}
	return p.master.Close()
}

// setRawTerminal sets the terminal attributes on fd to raw mode.
func setRawTerminal(fd uintptr) {
	var termios syscall.Termios
	// TCGETS = 0x5401
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, 0x5401, uintptr(unsafe.Pointer(&termios))); errno != 0 {
		return
	}

	// Raw mode: disable echo, canonical mode, extended input processing, signal chars
	termios.Iflag &^= syscall.IGNBRK | syscall.BRKINT | syscall.PARMRK | syscall.ISTRIP | syscall.INLCR | syscall.IGNCR | syscall.ICRNL | syscall.IXON
	termios.Oflag &^= syscall.OPOST
	termios.Lflag &^= syscall.ECHO | syscall.ECHONL | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	termios.Cflag &^= syscall.CSIZE | syscall.PARENB
	termios.Cflag |= syscall.CS8

	termios.Cc[syscall.VMIN] = 1
	termios.Cc[syscall.VTIME] = 0

	// TCSETS = 0x5402
	_, _, _ = syscall.Syscall(syscall.SYS_IOCTL, fd, 0x5402, uintptr(unsafe.Pointer(&termios)))
}
