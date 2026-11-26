// Package style provides a zero-dependency API for applying ANSI escape codes
// to terminal strings. It offers a chainable Style type, hex-to-truecolor
// conversion, and a Width helper that strips ANSI sequences before counting
// runes — replacing the lipgloss dependency for simple styling needs.
package style

import (
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ansiEscapeRe matches any ANSI SGR escape sequence of the form
// ESC [ <params> m so that Width can strip them before counting visible runes.
var ansiEscapeRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// Style holds styling attributes that can be applied to a string.
// All methods return a new Style value; the zero value is a valid, no-op style.
// Instances are safe for concurrent use because every mutating method copies
// the receiver before returning.
type Style struct {
	fg   string
	bold bool
}

// New returns a new empty Style with no attributes set.
func New() Style {
	return Style{}
}

// Foreground returns a copy of s with the foreground color set to color.
//
// Accepted formats:
//   - Hex strings like "#22c55e" — converted to ANSI truecolor (38;2;R;G;B).
//   - Bare integers like "9" or "12" — treated as ANSI 256-color (38;5;N).
func (s Style) Foreground(color string) Style {
	out := s
	out.fg = hexToANSI(color)
	return out
}

// Bold returns a copy of s with bold enabled when on is true, or disabled
// when on is false.
func (s Style) Bold(on bool) Style {
	out := s
	out.bold = on
	return out
}

// Render applies the style attributes to text and returns the result
// surrounded by the appropriate ANSI escape sequences. If no attributes are
// set the original text is returned unchanged (no escape sequences emitted).
func (s Style) Render(text string) string {
	var params []string

	if s.bold {
		params = append(params, "1")
	}
	if s.fg != "" {
		params = append(params, s.fg)
	}

	if len(params) == 0 {
		return text
	}

	open := fmt.Sprintf("\x1b[%sm", strings.Join(params, ";"))
	const reset = "\x1b[0m"

	slog.Debug("style.Render",
		slog.String("params", strings.Join(params, ";")),
		slog.Int("text_len", len(text)),
	)

	return open + text + reset
}

// Width returns the visible width of s in runes, stripping any ANSI escape
// sequences before counting. This mirrors lipgloss.Width for use in column
// alignment calculations.
func Width(s string) int {
	stripped := ansiEscapeRe.ReplaceAllString(s, "")
	return utf8.RuneCountInString(stripped)
}

// hexToANSI converts a color descriptor to an ANSI SGR color parameter
// fragment (the portion that appears between \x1b[ and m).
//
// Rules:
//   - Strings starting with "#" are parsed as 6-digit hex RGB and converted
//     to truecolor: "38;2;R;G;B".
//   - Strings containing only digits are treated as ANSI 256-color indices:
//     "38;5;N".
//   - Any other input is returned as-is so callers can pass raw SGR fragments
//     directly.
func hexToANSI(color string) string {
	slog.Debug("style.hexToANSI: converting color", slog.String("input", color))

	if hex, ok := strings.CutPrefix(color, "#"); ok {
		if len(hex) != 6 {
			slog.Debug("style.hexToANSI: invalid hex length, using as-is",
				slog.String("hex", hex),
				slog.Int("length", len(hex)),
			)
			return color
		}

		r, errR := strconv.ParseUint(hex[0:2], 16, 8)
		g, errG := strconv.ParseUint(hex[2:4], 16, 8)
		b, errB := strconv.ParseUint(hex[4:6], 16, 8)

		if errR != nil || errG != nil || errB != nil {
			slog.Debug("style.hexToANSI: hex parse error, using as-is",
				slog.String("hex", hex),
			)
			return color
		}

		result := fmt.Sprintf("38;2;%d;%d;%d", r, g, b)
		slog.Debug("style.hexToANSI: hex to truecolor",
			slog.String("input", color),
			slog.Uint64("r", r),
			slog.Uint64("g", g),
			slog.Uint64("b", b),
			slog.String("result", result),
		)
		return result
	}

	// Check whether the string is a bare integer (ANSI 256-color index).
	if _, err := strconv.ParseUint(color, 10, 16); err == nil {
		result := fmt.Sprintf("38;5;%s", color)
		slog.Debug("style.hexToANSI: integer to 256-color",
			slog.String("input", color),
			slog.String("result", result),
		)
		return result
	}

	// Unknown format — pass through unchanged so callers can supply raw
	// SGR fragments if needed.
	slog.Debug("style.hexToANSI: unknown format, passing through",
		slog.String("input", color),
	)
	return color
}

// Pre-defined package-level styles for common colours used across the project.
// Each variable is an immutable Style value; call additional chainable methods
// (e.g. Green.Bold(true)) to derive new styles without mutating the originals.
var (
	// Green is a style with a #22c55e foreground (Tailwind green-500).
	Green = New().Foreground("#22c55e")

	// Yellow is a style with a #eab308 foreground (Tailwind yellow-500).
	Yellow = New().Foreground("#eab308")

	// Red is a style with a #ef4444 foreground (Tailwind red-500).
	Red = New().Foreground("#ef4444")

	// Cyan is a style with a #06b6d4 foreground (Tailwind cyan-500).
	Cyan = New().Foreground("#06b6d4")

	// DimGray is a style with a #6b7280 foreground (Tailwind gray-500).
	DimGray = New().Foreground("#6b7280")

	// DimYellow is a style with a #ca8a04 foreground (Tailwind yellow-600).
	DimYellow = New().Foreground("#ca8a04")

	// Purple is a style with a #a78bfa foreground (Tailwind violet-400).
	Purple = New().Foreground("#a78bfa")

	// Gray is a style with a #9ca3af foreground (Tailwind gray-400).
	Gray = New().Foreground("#9ca3af")

	// BoldStyle is a style with bold text and no color.
	BoldStyle = New().Bold(true)
)
