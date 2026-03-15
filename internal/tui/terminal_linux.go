//go:build linux

package tui

import (
	"fmt"
	"log/slog"
	"os"
	"syscall"
	"unsafe"
)

// ─── Linux termios constants ───────────────────────────────────────────────────
//
// These values are derived from the Linux kernel headers:
//   include/uapi/asm-generic/ioctls.h  →  TCGETS / TCSETS
//   include/uapi/asm-generic/termbits.h  →  termios flag bits
//
// On Linux, syscall.Termios uses uint32 for Lflag and Iflag (unlike Darwin
// which uses uint64). The ioctl encoding on Linux does not embed the struct
// size in the request code, so the constants are simpler hex values.

const (
	// TCGETS is the Linux ioctl request code for reading terminal attributes.
	tcgets = 0x5401
	// TCSETS is the Linux ioctl request code for writing terminal attributes.
	tcsets = 0x5402

	// Lflag bits (local flags in struct termios.c_lflag).
	icanon = uint32(0x2)  // canonical (line-buffered) input mode
	echo   = uint32(0x8)  // echo input characters back to terminal
	isig   = uint32(0x1)  // generate signals for INTR, QUIT, SUSP

	// Iflag bits (input flags in struct termios.c_iflag).
	icrnl = uint32(0x100) // translate CR to NL on input
	ixon  = uint32(0x400) // enable XON/XOFF flow control

	// c_cc array indices for Linux.
	vmin  = 6 // minimum number of characters for non-canonical read
	vtime = 5 // timeout in tenths of a second for non-canonical read
)

// getTermios reads the current terminal attributes for the given file descriptor
// using the Linux TCGETS ioctl. Returns an error if the fd is not a terminal.
func getTermios(fd int) (syscall.Termios, error) {
	var t syscall.Termios
	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		uintptr(fd),
		tcgets,
		uintptr(unsafe.Pointer(&t)),
	)
	if errno != 0 {
		return t, fmt.Errorf("tui: TCGETS ioctl failed: %w", errno)
	}
	return t, nil
}

// setTermios applies the supplied terminal attributes to the given file
// descriptor using the Linux TCSETS ioctl.
func setTermios(fd int, t *syscall.Termios) error {
	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		uintptr(fd),
		tcsets,
		uintptr(unsafe.Pointer(t)),
	)
	if errno != 0 {
		return fmt.Errorf("tui: TCSETS ioctl failed: %w", errno)
	}
	return nil
}

// EnableRawMode puts stdin into raw mode and returns a restore function that
// reinstates the original terminal settings. Raw mode disables canonical
// (line-buffered) input and character echo so that individual keypresses are
// delivered immediately without requiring the user to press Enter.
//
// The caller must invoke the returned restore function — typically via defer —
// to avoid leaving the terminal in an unusable state if the process exits.
//
// Bits cleared on entry to raw mode:
//   - ICANON  — disable line buffering
//   - ECHO    — disable keystroke echo
//   - ISIG    — disable SIGINT/SIGQUIT generation (handled as plain bytes)
//   - ICRNL   — disable CR→NL translation
//   - IXON    — disable XON/XOFF flow control
//
// VMIN is set to 1 and VTIME to 0 so that Read on stdin blocks until at least
// one byte is available, with no inter-character timeout.
func EnableRawMode() (restore func(), err error) {
	fd := int(os.Stdin.Fd())

	slog.Debug("tui: reading current terminal attributes", slog.Int("fd", fd))
	orig, err := getTermios(fd)
	if err != nil {
		return nil, fmt.Errorf("tui: EnableRawMode: failed to read termios: %w", err)
	}

	raw := orig

	// Disable canonical mode, echo, and signal generation.
	raw.Lflag &^= icanon | echo | isig

	// Disable CR→NL translation and software flow control.
	raw.Iflag &^= icrnl | ixon

	// Read returns as soon as one byte is available; no timeout.
	raw.Cc[vmin] = 1
	raw.Cc[vtime] = 0

	slog.Debug("tui: applying raw mode terminal attributes")
	if err := setTermios(fd, &raw); err != nil {
		return nil, fmt.Errorf("tui: EnableRawMode: failed to set termios: %w", err)
	}

	restore = func() {
		slog.Debug("tui: restoring original terminal attributes")
		if restoreErr := setTermios(fd, &orig); restoreErr != nil {
			slog.Error("tui: failed to restore terminal attributes",
				slog.String("error", restoreErr.Error()),
			)
		}
	}

	slog.Info("tui: raw mode enabled")
	return restore, nil
}
