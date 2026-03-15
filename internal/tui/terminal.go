// Package tui provides a minimal terminal UI framework that operates without
// any external dependencies. It manages the alternate screen buffer, raw mode,
// cursor visibility, key reading, and a main event loop with periodic redraws.
//
// The package targets Unix systems and uses the syscall package to manipulate
// terminal attributes directly via platform-specific ioctl calls, eliminating
// the need for golang.org/x/term or any other third-party library.
//
// Platform-specific raw mode wiring lives in terminal_darwin.go and
// terminal_linux.go. All shared types and logic live in this file.

//go:build unix

package tui

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
)

// output is the writer used by all terminal control functions and the render
// loop. Tests replace this with a bytes.Buffer to capture ANSI sequences
// without touching a real terminal.
var output io.Writer = os.Stdout

// stdinReader is the reader used by readKeys. Tests replace this with a pipe
// to inject synthetic keystroke bytes without a real TTY.
var stdinReader io.Reader = os.Stdin

// enableRawMode is the function Run calls to enter raw mode. It is a variable
// so tests can replace it with a no-op that returns a no-op restore function,
// avoiding the need for a real TTY.
var enableRawMode = EnableRawMode

// ─── ANSI escape sequences ─────────────────────────────────────────────────────

const (
	// escAltScreenEnter switches to the xterm alternate screen buffer.
	escAltScreenEnter = "\x1b[?1049h"
	// escAltScreenExit restores the main screen buffer.
	escAltScreenExit = "\x1b[?1049l"
	// escHideCursor makes the text cursor invisible.
	escHideCursor = "\x1b[?25l"
	// escShowCursor makes the text cursor visible.
	escShowCursor = "\x1b[?25h"
	// escClearScreen erases the entire screen.
	escClearScreen = "\x1b[2J"
	// escCursorHome moves the cursor to row 1, column 1.
	escCursorHome = "\x1b[H"
)

// ─── Terminal control functions ────────────────────────────────────────────────

// EnterAltScreen switches to the alternate screen buffer. The terminal's main
// scroll-back history is preserved and restored when ExitAltScreen is called.
func EnterAltScreen() {
	slog.Debug("tui: entering alternate screen buffer")
	fmt.Fprint(output, escAltScreenEnter)
}

// ExitAltScreen restores the main screen buffer and scroll-back history.
func ExitAltScreen() {
	slog.Debug("tui: exiting alternate screen buffer")
	fmt.Fprint(output, escAltScreenExit)
}

// HideCursor makes the terminal cursor invisible. Call ShowCursor to restore it.
func HideCursor() {
	slog.Debug("tui: hiding cursor")
	fmt.Fprint(output, escHideCursor)
}

// ShowCursor restores terminal cursor visibility after a HideCursor call.
func ShowCursor() {
	slog.Debug("tui: showing cursor")
	fmt.Fprint(output, escShowCursor)
}

// ClearScreen erases all characters on the screen and moves the cursor to the
// top-left position (row 1, column 1).
func ClearScreen() {
	fmt.Fprint(output, escClearScreen+escCursorHome)
}

// MoveCursorHome moves the cursor to position (1, 1) without clearing the
// screen content. Use ClearScreen when a full repaint is required.
func MoveCursorHome() {
	fmt.Fprint(output, escCursorHome)
}

// ─── Key types ────────────────────────────────────────────────────────────────

// KeyType identifies the category of a keypress.
type KeyType int

const (
	// KeyChar represents a printable or otherwise plain character.
	KeyChar KeyType = iota
	// KeyCtrlC represents the Ctrl+C interrupt sequence (byte 0x03).
	KeyCtrlC
	// KeyEscape represents a bare Escape key (0x1b with no following bytes).
	KeyEscape
	// KeyEnter represents the Enter / Return key (byte 0x0d or 0x0a).
	KeyEnter
	// KeyUp represents the up-arrow key (ESC [ A).
	KeyUp
	// KeyDown represents the down-arrow key (ESC [ B).
	KeyDown
	// KeyUnknown represents any byte or escape sequence not explicitly handled.
	KeyUnknown
)

// Key holds a single parsed keypress event read from stdin.
type Key struct {
	// Char is the Unicode code point for KeyChar events. It is 0 for all other
	// KeyType values.
	Char rune
	// Type classifies the key event.
	Type KeyType
}

// ReadKey reads one logical keypress from reader. It handles the three-byte
// CSI escape sequences emitted by arrow keys (ESC [ A / B / C / D) and maps
// common control characters to their named KeyType constants.
//
// The function blocks until at least one byte is available. It is safe to call
// from a dedicated goroutine; it does not use any shared state.
func ReadKey(reader io.Reader) Key {
	buf := make([]byte, 1)
	if _, err := reader.Read(buf); err != nil {
		slog.Debug("tui: ReadKey: read error", slog.String("error", err.Error()))
		return Key{Type: KeyUnknown}
	}

	b := buf[0]
	slog.Debug("tui: ReadKey: received byte", slog.Int("byte", int(b)))

	switch b {
	case 0x03: // Ctrl+C
		return Key{Type: KeyCtrlC}

	case 0x0d, 0x0a: // CR or LF — Enter key
		return Key{Type: KeyEnter}

	case 0x1b: // ESC — could be a bare escape or a multi-byte sequence
		// Attempt a non-blocking peek at the next byte. Raw mode with VMIN=1
		// means the first Read above already consumed one byte. We now check
		// whether stdin has more data immediately available by issuing a second
		// Read from a dedicated small buffer. If stdin is a pipe or has no more
		// bytes buffered, this second Read will block — so we use a 1-byte read
		// that will only have more data if the escape sequence was sent atomically
		// (which terminals always do for arrow keys).
		seq := make([]byte, 2)
		n, err := reader.Read(seq[:1])
		if err != nil || n == 0 {
			// Bare escape key — no following bytes arrived.
			return Key{Type: KeyEscape}
		}

		if seq[0] != '[' {
			// Unknown two-byte sequence starting with ESC.
			slog.Debug("tui: ReadKey: unrecognised escape sequence",
				slog.Int("second_byte", int(seq[0])),
			)
			return Key{Type: KeyUnknown}
		}

		// Read the final byte of the CSI sequence: ESC [ <X>.
		n, err = reader.Read(seq[1:2])
		if err != nil || n == 0 {
			return Key{Type: KeyUnknown}
		}

		switch seq[1] {
		case 'A':
			slog.Debug("tui: ReadKey: arrow up")
			return Key{Type: KeyUp}
		case 'B':
			slog.Debug("tui: ReadKey: arrow down")
			return Key{Type: KeyDown}
		default:
			slog.Debug("tui: ReadKey: unrecognised CSI sequence",
				slog.String("final_byte", string(seq[1])),
			)
			return Key{Type: KeyUnknown}
		}

	default:
		return Key{Char: rune(b), Type: KeyChar}
	}
}

// ─── Model interface ──────────────────────────────────────────────────────────

// Model is the interface that application models must implement to integrate
// with the Program event loop. The single method returns the complete screen
// content as a string; the Program writes it verbatim to stdout on every
// redraw.
type Model interface {
	// View returns the full screen content as a string. It is called on every
	// refresh and must be a pure, side-effect-free function of model state.
	View() string
}

// ─── Program ─────────────────────────────────────────────────────────────────

// Program manages the terminal UI lifecycle: raw mode, alternate screen buffer,
// cursor visibility, a key-reading goroutine, and a select-driven render loop.
//
// Typical usage:
//
//	p := tui.NewProgram(myModel)
//	p.OnKey(func(k tui.Key) bool {
//	    if k.Type == tui.KeyCtrlC { return true }
//	    myModel.HandleKey(k)
//	    return false
//	})
//	if err := p.Run(); err != nil {
//	    log.Fatal(err)
//	}
type Program struct {
	model      Model
	quit       chan struct{}
	keys       chan Key
	refresh    chan struct{}
	onKey      func(Key) bool
	once       sync.Once    // guards Quit so it is safe to call multiple times
	readerDone chan struct{} // closed when the readKeys goroutine exits
}

// NewProgram constructs a Program backed by the given Model. The program does
// not start until Run is called.
func NewProgram(model Model) *Program {
	slog.Debug("tui: NewProgram: creating program")
	return &Program{
		model:      model,
		quit:       make(chan struct{}),
		keys:       make(chan Key, 16),
		refresh:    make(chan struct{}, 1),
		readerDone: make(chan struct{}),
	}
}

// OnKey registers a callback that is invoked on the main goroutine whenever a
// keypress is received. Returning true from the callback causes the program to
// exit cleanly. Only the most recently registered callback is active; call
// OnKey again to replace it.
func (p *Program) OnKey(fn func(Key) bool) {
	slog.Debug("tui: OnKey: key handler registered")
	p.onKey = fn
}

// Quit signals the program to exit. It is safe to call from any goroutine and
// is idempotent — multiple calls have no additional effect.
func (p *Program) Quit() {
	p.once.Do(func() {
		slog.Info("tui: Quit: quit signal sent")
		close(p.quit)
	})
}

// Refresh requests a screen redraw. If a redraw is already queued the call is
// a no-op (the channel is buffered with capacity 1). It is safe to call from
// any goroutine.
func (p *Program) Refresh() {
	select {
	case p.refresh <- struct{}{}:
		slog.Debug("tui: Refresh: redraw queued")
	default:
		// A redraw is already pending; no need to queue another.
	}
}

// Run starts the TUI event loop and blocks until Quit is called or the OnKey
// callback returns true. On return, the terminal is fully restored regardless
// of whether Run exits normally or via a panic.
//
// Run performs the following initialisation steps in order:
//  1. Enables raw mode on stdin, registering a deferred restore.
//  2. Enters the alternate screen buffer.
//  3. Hides the cursor.
//  4. Launches a goroutine that reads keypresses and forwards them to the
//     internal keys channel.
//  5. Performs an initial render.
//  6. Enters the select loop, handling key events and refresh requests.
//
// Cleanup (show cursor, exit alt screen, restore terminal mode) is performed
// via defer, so it executes even if Run returns due to a panic in the key
// callback or model.
func (p *Program) Run() error {
	slog.Info("tui: Run: starting program")

	restore, err := enableRawMode()
	if err != nil {
		return fmt.Errorf("tui: Run: %w", err)
	}
	defer func() {
		slog.Debug("tui: Run: deferred cleanup: restoring terminal")
		ShowCursor()
		ExitAltScreen()
		restore()
		slog.Info("tui: Run: terminal restored")
	}()

	EnterAltScreen()
	HideCursor()

	// Start the key-reading goroutine. It runs until the program quits.
	go p.readKeys()

	// Initial render before entering the select loop.
	p.render()

	slog.Debug("tui: Run: entering event loop")
	for {
		select {
		case <-p.quit:
			slog.Info("tui: Run: quit channel closed, exiting event loop")
			return nil

		case k, ok := <-p.keys:
			if !ok {
				slog.Info("tui: Run: keys channel closed, exiting event loop")
				return nil
			}
			slog.Debug("tui: Run: key event received",
				slog.Int("type", int(k.Type)),
				slog.String("char", string(k.Char)),
			)
			if p.onKey != nil {
				if shouldQuit := p.onKey(k); shouldQuit {
					slog.Info("tui: Run: OnKey callback requested quit")
					return nil
				}
			}
			p.render()

		case <-p.refresh:
			slog.Debug("tui: Run: refresh event received")
			p.render()
		}
	}
}

// render clears the screen and writes the current model view to stdout.
// It is called exclusively on the main goroutine from the event loop.
func (p *Program) render() {
	ClearScreen()
	view := p.model.View()
	_, err := fmt.Fprint(output, view)
	if err != nil {
		slog.Error("tui: render: failed to write view",
			slog.String("error", err.Error()),
		)
	}
}

// readKeys runs in a dedicated goroutine and continuously reads keypresses from
// stdinReader, forwarding each parsed Key to the keys channel. It exits when
// the quit channel is closed or when the reader is closed/errors. It closes
// the readerDone channel before returning so callers can synchronise.
func (p *Program) readKeys() {
	slog.Debug("tui: readKeys goroutine started")
	defer func() {
		slog.Debug("tui: readKeys goroutine exiting")
		close(p.readerDone)
	}()

	// Capture stdinReader at goroutine start so tests can safely restore the
	// global after Run returns without racing with this goroutine.
	reader := stdinReader
	for {
		// Check for quit before blocking on Read.
		select {
		case <-p.quit:
			slog.Debug("tui: readKeys: quit signalled, exiting goroutine")
			return
		default:
		}

		k := ReadKey(reader)
		slog.Debug("tui: readKeys: parsed key",
			slog.Int("type", int(k.Type)),
			slog.String("char", string(k.Char)),
		)

		select {
		case p.keys <- k:
		case <-p.quit:
			slog.Debug("tui: readKeys: quit signalled while sending key, exiting goroutine")
			return
		}
	}
}
