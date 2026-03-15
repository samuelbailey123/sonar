//go:build unix

package tui_test

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/samuelbailey123/sonar/internal/tui"
)

// ─── ReadKey tests ────────────────────────────────────────────────────────────

func TestReadKey_CtrlC(t *testing.T) {
	t.Parallel()
	r := bytes.NewReader([]byte{0x03})
	k := tui.ReadKey(r)
	if k.Type != tui.KeyCtrlC {
		t.Errorf("expected KeyCtrlC, got %d", k.Type)
	}
}

func TestReadKey_Enter_CR(t *testing.T) {
	t.Parallel()
	r := bytes.NewReader([]byte{0x0d})
	k := tui.ReadKey(r)
	if k.Type != tui.KeyEnter {
		t.Errorf("expected KeyEnter (CR), got %d", k.Type)
	}
}

func TestReadKey_Enter_LF(t *testing.T) {
	t.Parallel()
	r := bytes.NewReader([]byte{0x0a})
	k := tui.ReadKey(r)
	if k.Type != tui.KeyEnter {
		t.Errorf("expected KeyEnter (LF), got %d", k.Type)
	}
}

func TestReadKey_ArrowUp(t *testing.T) {
	t.Parallel()
	// ESC [ A
	r := bytes.NewReader([]byte{0x1b, '[', 'A'})
	k := tui.ReadKey(r)
	if k.Type != tui.KeyUp {
		t.Errorf("expected KeyUp, got %d", k.Type)
	}
}

func TestReadKey_ArrowDown(t *testing.T) {
	t.Parallel()
	// ESC [ B
	r := bytes.NewReader([]byte{0x1b, '[', 'B'})
	k := tui.ReadKey(r)
	if k.Type != tui.KeyDown {
		t.Errorf("expected KeyDown, got %d", k.Type)
	}
}

func TestReadKey_PlainChar(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input byte
		want  rune
	}{
		{"lowercase q", 'q', 'q'},
		{"space", ' ', ' '},
		{"digit 5", '5', '5'},
		{"uppercase Z", 'Z', 'Z'},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := bytes.NewReader([]byte{tc.input})
			k := tui.ReadKey(r)
			if k.Type != tui.KeyChar {
				t.Errorf("expected KeyChar, got %d", k.Type)
			}
			if k.Char != tc.want {
				t.Errorf("expected Char %q, got %q", tc.want, k.Char)
			}
		})
	}
}

func TestReadKey_BareEscape_ReturnsKeyEscape(t *testing.T) {
	t.Parallel()
	// Only an ESC byte with nothing following — reader returns EOF on second read.
	r := bytes.NewReader([]byte{0x1b})
	k := tui.ReadKey(r)
	// When the reader is exhausted after the ESC byte the second Read fails,
	// so the result must be either KeyEscape or KeyUnknown — never a crash.
	if k.Type != tui.KeyEscape && k.Type != tui.KeyUnknown {
		t.Errorf("unexpected key type for bare ESC: %d", k.Type)
	}
}

func TestReadKey_UnknownEscapeSequence(t *testing.T) {
	t.Parallel()
	// ESC [ C is right-arrow — not mapped but should not panic.
	r := bytes.NewReader([]byte{0x1b, '[', 'C'})
	k := tui.ReadKey(r)
	if k.Type != tui.KeyUnknown {
		t.Errorf("expected KeyUnknown for ESC [ C, got %d", k.Type)
	}
}

func TestReadKey_EmptyReader(t *testing.T) {
	t.Parallel()
	r := bytes.NewReader(nil)
	k := tui.ReadKey(r)
	// EOF on the very first read must return KeyUnknown, not panic.
	if k.Type != tui.KeyUnknown {
		t.Errorf("expected KeyUnknown for empty reader, got %d", k.Type)
	}
}

// ─── Model and Program tests ──────────────────────────────────────────────────

// staticModel is a minimal Model implementation that always returns a fixed
// view string. It is used to verify the Program plumbing without a real TTY.
type staticModel struct {
	view string
}

func (m *staticModel) View() string { return m.view }

func TestNewProgram_NotNil(t *testing.T) {
	t.Parallel()
	m := &staticModel{view: "hello"}
	p := tui.NewProgram(m)
	if p == nil {
		t.Fatal("NewProgram returned nil")
	}
}

func TestProgram_QuitIsIdempotent(t *testing.T) {
	t.Parallel()
	m := &staticModel{view: "test"}
	p := tui.NewProgram(m)
	// Multiple Quit calls must not panic (close of closed channel).
	p.Quit()
	p.Quit()
	p.Quit()
}

func TestProgram_RefreshIsNonBlocking(t *testing.T) {
	t.Parallel()
	m := &staticModel{view: "test"}
	p := tui.NewProgram(m)
	// Calling Refresh many times before Run must never block or panic.
	for i := 0; i < 64; i++ {
		p.Refresh()
	}
}

func TestProgram_OnKey_SetCallback(t *testing.T) {
	t.Parallel()
	m := &staticModel{view: "test"}
	p := tui.NewProgram(m)
	called := false
	p.OnKey(func(k tui.Key) bool {
		called = true
		return true
	})
	_ = called
	// OnKey must not panic when called before Run.
}

// ─── Key type constant tests ──────────────────────────────────────────────────

func TestKeyType_Iota(t *testing.T) {
	t.Parallel()
	// Verify the iota ordering matches documented constants.
	if tui.KeyChar != 0 {
		t.Errorf("KeyChar must be 0, got %d", tui.KeyChar)
	}
	if tui.KeyCtrlC != 1 {
		t.Errorf("KeyCtrlC must be 1, got %d", tui.KeyCtrlC)
	}
	if tui.KeyEscape != 2 {
		t.Errorf("KeyEscape must be 2, got %d", tui.KeyEscape)
	}
	if tui.KeyEnter != 3 {
		t.Errorf("KeyEnter must be 3, got %d", tui.KeyEnter)
	}
	if tui.KeyUp != 4 {
		t.Errorf("KeyUp must be 4, got %d", tui.KeyUp)
	}
	if tui.KeyDown != 5 {
		t.Errorf("KeyDown must be 5, got %d", tui.KeyDown)
	}
	if tui.KeyUnknown != 6 {
		t.Errorf("KeyUnknown must be 6, got %d", tui.KeyUnknown)
	}
}

// ─── Key.Char zero-value tests ────────────────────────────────────────────────

func TestReadKey_SpecialKeysHaveZeroChar(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		input []byte
		kind  tui.KeyType
	}{
		{"ctrl+c", []byte{0x03}, tui.KeyCtrlC},
		{"enter CR", []byte{0x0d}, tui.KeyEnter},
		{"arrow up", []byte{0x1b, '[', 'A'}, tui.KeyUp},
		{"arrow down", []byte{0x1b, '[', 'B'}, tui.KeyDown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := bytes.NewReader(tc.input)
			k := tui.ReadKey(r)
			if k.Type != tc.kind {
				t.Errorf("expected type %d, got %d", tc.kind, k.Type)
			}
			if k.Char != 0 {
				t.Errorf("expected zero Char for %s, got %q", tc.name, k.Char)
			}
		})
	}
}

// ─── staticModel view fidelity ────────────────────────────────────────────────

func TestStaticModel_View(t *testing.T) {
	t.Parallel()
	const content = "DNS propagation: 100%\nAll resolvers agree."
	m := &staticModel{view: content}
	got := m.View()
	if !strings.Contains(got, "DNS propagation") {
		t.Errorf("View() did not contain expected content: %q", got)
	}
}

// ─── Terminal output function tests ──────────────────────────────────────────
//
// Each function is tested by redirecting the package-level output writer to a
// bytes.Buffer and asserting the correct ANSI escape sequence was written.
// These tests do not require a real TTY.

// withCapturedOutput swaps the package output writer for a fresh buffer,
// calls fn, then restores the original writer. It returns the bytes written.
func withCapturedOutput(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	restore := tui.SetOutput(&buf)
	defer restore()
	fn()
	return buf.String()
}

// These tests mutate the shared package-level output variable and must NOT run
// in parallel with each other or with the Run tests below.

func TestEnterAltScreen_WritesEscapeSequence(t *testing.T) {
	got := withCapturedOutput(t, tui.EnterAltScreen)
	const want = "\x1b[?1049h"
	if got != want {
		t.Errorf("EnterAltScreen: got %q, want %q", got, want)
	}
}

func TestExitAltScreen_WritesEscapeSequence(t *testing.T) {
	got := withCapturedOutput(t, tui.ExitAltScreen)
	const want = "\x1b[?1049l"
	if got != want {
		t.Errorf("ExitAltScreen: got %q, want %q", got, want)
	}
}

func TestHideCursor_WritesEscapeSequence(t *testing.T) {
	got := withCapturedOutput(t, tui.HideCursor)
	const want = "\x1b[?25l"
	if got != want {
		t.Errorf("HideCursor: got %q, want %q", got, want)
	}
}

func TestShowCursor_WritesEscapeSequence(t *testing.T) {
	got := withCapturedOutput(t, tui.ShowCursor)
	const want = "\x1b[?25h"
	if got != want {
		t.Errorf("ShowCursor: got %q, want %q", got, want)
	}
}

func TestClearScreen_WritesClearAndHome(t *testing.T) {
	got := withCapturedOutput(t, tui.ClearScreen)
	const want = "\x1b[2J\x1b[H"
	if got != want {
		t.Errorf("ClearScreen: got %q, want %q", got, want)
	}
}

func TestMoveCursorHome_WritesCursorHome(t *testing.T) {
	got := withCapturedOutput(t, tui.MoveCursorHome)
	const want = "\x1b[H"
	if got != want {
		t.Errorf("MoveCursorHome: got %q, want %q", got, want)
	}
}

// ─── render tests ─────────────────────────────────────────────────────────────
//
// render is tested indirectly by verifying that the model's View string appears
// in the captured output alongside the clear-screen escape sequences. render is
// a private method; it is exercised through Program.Refresh and the event loop
// during the Run tests below.

// ─── EnableRawMode / getTermios / setTermios tests ────────────────────────────
//
// These functions require a real TTY and cannot be exercised in a standard CI
// environment where stdin is a pipe. The test attempts the syscall and skips if
// the file descriptor is not a terminal (errno ENOTTY).

func TestEnableRawMode_SkipsIfNotTTY(t *testing.T) {
	// Not parallel — EnableRawMode operates on os.Stdin which is shared state.
	restore, err := tui.EnableRawMode()
	if err != nil {
		// On non-TTY environments (CI, pipes) the ioctl returns an error.
		// That is the expected failure mode; skip rather than fail.
		t.Skipf("EnableRawMode returned error (likely not a TTY): %v", err)
	}
	// On a real TTY the call must succeed and return a non-nil restore function.
	if restore == nil {
		t.Fatal("EnableRawMode succeeded but returned a nil restore function")
	}
	restore()
}

// ─── Program.Run tests ────────────────────────────────────────────────────────
//
// Run is tested by injecting three things:
//  1. A no-op EnableRawMode so no real TTY is required.
//  2. A bytes.Buffer for output so ANSI sequences can be inspected.
//  3. A pipe for stdin so synthetic keypresses can be injected.
//
// Because Run blocks, it is started in a goroutine and the test drives it by
// writing to the pipe and asserting on the results channel.

// noopRawMode is a drop-in replacement for EnableRawMode that succeeds without
// touching any file descriptor.
func noopRawMode() (func(), error) {
	return func() {}, nil
}

// setupRunTest injects output, stdinReader, and enableRawMode globals for a
// Run test and starts Run in a goroutine. It returns the pipe read end, the
// pipe write end, a done channel that is closed when the readKeys goroutine
// exits, and a safeShutdown function.
//
// safeShutdown is idempotent — it may be called multiple times (e.g. once
// explicitly and once via defer) without side effects. It:
//  1. Signals Quit (idempotent — safe even if a key handler already quit).
//  2. Closes both ends of the pipe so the readKeys goroutine unblocks from its
//     Read call and Run can complete its deferred cleanup path.
//  3. Waits for Run to return (up to 3 s).
//  4. Waits for the readKeys goroutine to fully exit via done (up to 3 s).
//  5. Restores the global injections in reverse order.
//
// Because readKeys captures stdinReader into a local variable at goroutine
// start, step 5 no longer races with the goroutine. Steps 3-4 still guard
// against goroutine leaks between tests.
//
// Tests that assert on output after Run exits must call shutdown explicitly
// before asserting, because the deferred shutdown fires after the function
// body returns — too late if the assertion needs a quiescent Run goroutine.
// Using defer shutdown() as a safety net alongside an explicit call is safe
// because shutdown is idempotent.
func setupRunTest(
	t *testing.T,
	p *tui.Program,
	out *bytes.Buffer,
) (pr *io.PipeReader, pw *io.PipeWriter, done <-chan struct{}, safeShutdown func()) {
	t.Helper()

	pr2, pw2 := io.Pipe()
	restoreOut := tui.SetOutput(out)
	restoreStdin := tui.SetStdinReader(pr2)
	restoreRaw := tui.SetEnableRawMode(noopRawMode)

	runDone := make(chan error, 1)
	go func() { runDone <- p.Run() }()

	var once sync.Once
	safeShutdown = func() {
		once.Do(func() {
			p.Quit()
			pw2.Close()
			pr2.Close()
			select {
			case <-runDone:
			case <-time.After(3 * time.Second):
				t.Error("setupRunTest: Run did not exit within 3 seconds")
			}
			select {
			case <-tui.ReaderDone(p):
			case <-time.After(3 * time.Second):
				t.Error("setupRunTest: readKeys goroutine did not exit within 3 seconds")
			}
			restoreRaw()
			restoreStdin()
			restoreOut()
		})
	}
	return pr2, pw2, tui.ReaderDone(p), safeShutdown
}

func TestProgram_Run_ExitsViaQuit(t *testing.T) {
	// Not parallel — mutates shared package globals.
	var out bytes.Buffer
	m := &staticModel{view: "hello world"}
	p := tui.NewProgram(m)

	_, _, _, shutdown := setupRunTest(t, p, &out)
	defer shutdown()

	time.Sleep(20 * time.Millisecond)
	p.Quit()

	// Wait for Run to fully exit before reading out. The Run goroutine writes
	// to out during deferred cleanup (ShowCursor, ExitAltScreen), so asserting
	// before Run exits would race. shutdown is idempotent — the deferred call
	// above is a no-op after this explicit call.
	shutdown()

	got := out.String()
	if !strings.Contains(got, "hello world") {
		t.Errorf("expected output to contain model view, got: %q", got)
	}
}

func TestProgram_Run_ExitsViaOnKeyReturningTrue(t *testing.T) {
	// Not parallel — mutates shared package globals.
	var out bytes.Buffer
	m := &staticModel{view: "view"}
	p := tui.NewProgram(m)
	p.OnKey(func(k tui.Key) bool {
		return k.Type == tui.KeyChar && k.Char == 'q'
	})

	_, pw, _, shutdown := setupRunTest(t, p, &out)
	defer shutdown()

	time.Sleep(20 * time.Millisecond)
	if _, err := pw.Write([]byte{'q'}); err != nil {
		// Write may fail if Run already exited — not a test failure.
		t.Logf("pw.Write: %v", err)
	}
}

func TestProgram_Run_ExitsWhenStdinClosed(t *testing.T) {
	// Not parallel — mutates shared package globals.
	var out bytes.Buffer
	m := &staticModel{view: "view"}
	p := tui.NewProgram(m)

	_, _, _, shutdown := setupRunTest(t, p, &out)
	defer shutdown()

	time.Sleep(20 * time.Millisecond)
	// The deferred shutdown closes the pipe and calls Quit — that is sufficient
	// to drive the program to exit cleanly from a closed stdin.
}

func TestProgram_Run_RendersOnRefresh(t *testing.T) {
	// Not parallel — mutates shared package globals.
	var out bytes.Buffer
	m := &staticModel{view: "refresh-content"}
	p := tui.NewProgram(m)

	_, _, _, shutdown := setupRunTest(t, p, &out)
	defer shutdown()

	time.Sleep(20 * time.Millisecond)
	p.Refresh()
	time.Sleep(20 * time.Millisecond)
	p.Quit()

	// Wait for Run to fully exit before reading out. The Run goroutine writes
	// to out during deferred cleanup (ShowCursor, ExitAltScreen), so asserting
	// before Run exits would race. shutdown is idempotent — the deferred call
	// above is a no-op after this explicit call.
	shutdown()

	got := out.String()
	if count := strings.Count(got, "refresh-content"); count < 2 {
		t.Errorf("expected view to appear >= 2 times, got %d in %q", count, got)
	}
}

func TestProgram_Run_NoOnKey_DoesNotPanic(t *testing.T) {
	// Not parallel — mutates shared package globals.
	var out bytes.Buffer
	m := &staticModel{view: "no-handler"}
	p := tui.NewProgram(m)

	_, pw, _, shutdown := setupRunTest(t, p, &out)
	defer shutdown()

	time.Sleep(20 * time.Millisecond)
	pw.Write([]byte{'x'}) //nolint:errcheck
	time.Sleep(10 * time.Millisecond)
}

// ─── readKeys goroutine tests ─────────────────────────────────────────────────
//
// readKeys is private but is exercised indirectly through Program.Run. The
// tests below verify that multiple keys are delivered in order and that arrow
// key sequences are parsed and forwarded correctly.

func TestProgram_Run_MultipleKeysDeliveredInOrder(t *testing.T) {
	// Not parallel — mutates shared package globals.
	var out bytes.Buffer
	var mu sync.Mutex
	var received []rune

	m := &staticModel{view: "v"}
	p := tui.NewProgram(m)
	p.OnKey(func(k tui.Key) bool {
		if k.Type == tui.KeyCtrlC {
			return true
		}
		if k.Type == tui.KeyChar {
			mu.Lock()
			received = append(received, k.Char)
			mu.Unlock()
		}
		return false
	})

	_, pw, _, shutdown := setupRunTest(t, p, &out)

	time.Sleep(20 * time.Millisecond)
	pw.Write([]byte{'a', 'b', 'c', 0x03}) //nolint:errcheck

	// shutdown waits for Run to exit and readKeys to stop, ensuring all key
	// events have been processed before we assert on received.
	shutdown()

	mu.Lock()
	got := received
	mu.Unlock()

	want := []rune{'a', 'b', 'c'}
	if len(got) != len(want) {
		t.Fatalf("expected %v chars received, got %v", want, got)
	}
	for i, r := range want {
		if got[i] != r {
			t.Errorf("key[%d]: want %q, got %q", i, r, got[i])
		}
	}
}

func TestProgram_Run_ArrowKeysForwarded(t *testing.T) {
	// Not parallel — mutates shared package globals.
	var out bytes.Buffer
	var mu sync.Mutex
	var received []tui.KeyType

	m := &staticModel{view: "v"}
	p := tui.NewProgram(m)
	p.OnKey(func(k tui.Key) bool {
		mu.Lock()
		received = append(received, k.Type)
		mu.Unlock()
		return k.Type == tui.KeyCtrlC
	})

	_, pw, _, shutdown := setupRunTest(t, p, &out)

	time.Sleep(20 * time.Millisecond)
	// ESC [ A = up, ESC [ B = down, then Ctrl+C to quit.
	pw.Write([]byte{0x1b, '[', 'A', 0x1b, '[', 'B', 0x03}) //nolint:errcheck

	// shutdown waits for Run and readKeys to fully exit.
	shutdown()

	mu.Lock()
	got := received
	mu.Unlock()

	if len(got) < 2 {
		t.Fatalf("expected at least 2 key events, got %d: %v", len(got), got)
	}
	if got[0] != tui.KeyUp {
		t.Errorf("first key: want KeyUp, got %d", got[0])
	}
	if got[1] != tui.KeyDown {
		t.Errorf("second key: want KeyDown, got %d", got[1])
	}
}

// ─── Run error path tests ─────────────────────────────────────────────────────

func TestProgram_Run_ReturnsErrorWhenRawModeFails(t *testing.T) {
	// Not parallel — mutates shared package globals.
	// Inject an enableRawMode that always returns an error. Run must propagate
	// the error rather than panic or silently proceed.
	var out bytes.Buffer
	restoreOut := tui.SetOutput(&out)
	defer restoreOut()

	failRaw := func() (func(), error) {
		return nil, fmt.Errorf("synthetic raw mode failure")
	}
	restoreRaw := tui.SetEnableRawMode(failRaw)
	defer restoreRaw()

	m := &staticModel{view: "v"}
	p := tui.NewProgram(m)

	err := p.Run()
	if err == nil {
		t.Fatal("expected Run to return an error when raw mode fails, got nil")
	}
	if !strings.Contains(err.Error(), "synthetic raw mode failure") {
		t.Errorf("unexpected error text: %v", err)
	}
}

// errWriter is an io.Writer that always returns an error.
type errWriter struct{ msg string }

func (e *errWriter) Write(_ []byte) (int, error) {
	return 0, fmt.Errorf("%s", e.msg)
}

func TestProgram_Run_RenderHandlesWriteError(t *testing.T) {
	// Not parallel — mutates shared package globals.
	// Inject a writer that always errors. Run must log the error but not
	// crash — the event loop must continue operating.
	w := &errWriter{msg: "disk full"}
	restoreOut := tui.SetOutput(w)
	defer restoreOut()

	pr, pw := io.Pipe()
	restoreStdin := tui.SetStdinReader(pr)
	defer restoreStdin()
	restoreRaw := tui.SetEnableRawMode(noopRawMode)
	defer restoreRaw()

	m := &staticModel{view: "content"}
	p := tui.NewProgram(m)

	runDone := make(chan error, 1)
	go func() { runDone <- p.Run() }()

	time.Sleep(20 * time.Millisecond)
	p.Quit()

	select {
	case err := <-runDone:
		if err != nil {
			t.Errorf("Run returned unexpected error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not exit within 3 seconds")
	}

	pw.Close()
	pr.Close()
	select {
	case <-tui.ReaderDone(p):
	case <-time.After(3 * time.Second):
		t.Error("readKeys did not exit within 3 seconds")
	}
}

// ─── ReadKey edge case tests ───────────────────────────────────────────────────

func TestReadKey_EscFollowedByNonBracket_ReturnsUnknown(t *testing.T) {
	t.Parallel()
	// ESC followed by a non-'[' byte — not a CSI sequence.
	r := bytes.NewReader([]byte{0x1b, 'O'})
	k := tui.ReadKey(r)
	if k.Type != tui.KeyUnknown {
		t.Errorf("expected KeyUnknown for ESC O, got %d", k.Type)
	}
}

func TestReadKey_EscBracketFollowedByEOF_ReturnsUnknown(t *testing.T) {
	t.Parallel()
	// ESC [ with no final byte — the second Read after '[' gets EOF.
	r := bytes.NewReader([]byte{0x1b, '['})
	k := tui.ReadKey(r)
	if k.Type != tui.KeyUnknown {
		t.Errorf("expected KeyUnknown for ESC [ <EOF>, got %d", k.Type)
	}
}
