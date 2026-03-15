//go:build darwin

package tui

import (
	"fmt"
	"log/slog"
	"os"
	"syscall"
	"unsafe"
)

// ─── Darwin termios constants ──────────────────────────────────────────────────
//
// These values are derived from the macOS SDK headers:
//   /usr/include/sys/ttycom.h  →  _IOR('t', 19, struct termios)
//   /usr/include/sys/ttycom.h  →  _IOW('t', 20, struct termios)
//
// The struct termios on Darwin is 60 bytes (4 × uint64 flags + 20 cc bytes +
// 4-byte pad + 2 × uint64 speeds) — the kernel ioctl encoding embeds the size.
// The precomputed hex literals below match what the C preprocessor resolves to
// on arm64 and amd64 Darwin.
//
// On Darwin, syscall.Termios uses uint64 for Lflag and Iflag.

const (
	// tiocgeta is the Darwin ioctl request code for reading terminal attributes.
	tiocgeta = 0x40487413
	// tiocseta is the Darwin ioctl request code for writing terminal attributes.
	tiocseta = 0x80487414

	// Lflag bits (local flags in struct termios.c_lflag).
	icanon = uint64(0x00000100) // canonical (line-buffered) input mode
	echo   = uint64(0x00000008) // echo input characters back to terminal
	isig   = uint64(0x00000080) // generate signals for INTR, QUIT, SUSP

	// Iflag bits (input flags in struct termios.c_iflag).
	icrnl = uint64(0x00000100) // translate CR to NL on input
	ixon  = uint64(0x00000200) // enable XON/XOFF flow control

	// c_cc array indices for Darwin.
	vmin  = 16 // minimum number of characters for non-canonical read
	vtime = 17 // timeout in tenths of a second for non-canonical read
)

// getTermios reads the current terminal attributes for the given file descriptor
// using the Darwin TIOCGETA ioctl. Returns an error if the fd is not a terminal.
func getTermios(fd int) (syscall.Termios, error) {
	var t syscall.Termios
	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		uintptr(fd),
		tiocgeta,
		uintptr(unsafe.Pointer(&t)),
	)
	if errno != 0 {
		return t, fmt.Errorf("tui: TIOCGETA ioctl failed: %w", errno)
	}
	return t, nil
}

// setTermios applies the supplied terminal attributes to the given file
// descriptor using the Darwin TIOCSETA ioctl.
func setTermios(fd int, t *syscall.Termios) error {
	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		uintptr(fd),
		tiocseta,
		uintptr(unsafe.Pointer(t)),
	)
	if errno != 0 {
		return fmt.Errorf("tui: TIOCSETA ioctl failed: %w", errno)
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
