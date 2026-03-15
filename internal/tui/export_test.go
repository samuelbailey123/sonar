//go:build unix

package tui

// This file is part of package tui (not tui_test) so it can access unexported
// package-level variables. It is compiled only when running tests because Go
// only compiles *_test.go files during test builds.

import "io"

// SetOutput replaces the package-level output writer used by all terminal
// control functions and the render loop. It returns a restore function that
// resets output to its original value so callers can defer cleanup without
// leaking state between parallel tests.
func SetOutput(w io.Writer) (restore func()) {
	prev := output
	output = w
	return func() { output = prev }
}

// SetStdinReader replaces the package-level stdin reader used by readKeys. It
// returns a restore function that resets stdinReader to its original value.
func SetStdinReader(r io.Reader) (restore func()) {
	prev := stdinReader
	stdinReader = r
	return func() { stdinReader = prev }
}

// SetEnableRawMode replaces the enableRawMode function used by Run. Tests
// inject a no-op that returns immediately so Run can execute without a real
// TTY. It returns a restore function that reinstates the original value.
func SetEnableRawMode(fn func() (func(), error)) (restore func()) {
	prev := enableRawMode
	enableRawMode = fn
	return func() { enableRawMode = prev }
}

// ReaderDone returns a channel that is closed when the readKeys goroutine
// exits. Tests block on this channel after closing the stdin pipe to guarantee
// the goroutine has fully stopped before restoring the stdinReader global.
func ReaderDone(p *Program) <-chan struct{} {
	return p.readerDone
}

