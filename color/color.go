package color

import (
	"fmt"
	"os"
)

const (
	reset   = "\033[0m"
	green   = "\033[32m"
	yellow  = "\033[33m"
	red     = "\033[31m"
	magenta = "\033[35m"
	orange  = "\033[38;5;208m"
)

// enabled reports whether ANSI colour codes should be emitted. Colours are
// suppressed when NO_COLOR is set (https://no-color.org) or when stdout is not
// a terminal (e.g. redirected to a file or a pipe).
var enabled = detectColor()

func detectColor() bool {
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return false
	}
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// SetEnabled overrides colour detection (useful for tests).
func SetEnabled(v bool) { enabled = v }

func wrap(code, s string) string {
	if !enabled {
		return s
	}
	return code + s + reset
}

// Green wraps s in green ANSI codes.
func Green(s string) string { return wrap(green, s) }

// Yellow wraps s in yellow ANSI codes.
func Yellow(s string) string { return wrap(yellow, s) }

// Red wraps s in red ANSI codes.
func Red(s string) string { return wrap(red, s) }

// Magenta wraps s in magenta ANSI codes.
func Magenta(s string) string { return wrap(magenta, s) }

// Orange wraps s in orange ANSI codes (256-color).
func Orange(s string) string { return wrap(orange, s) }

// Greenf is like fmt.Sprintf with green wrapping.
func Greenf(format string, a ...any) string { return Green(fmt.Sprintf(format, a...)) }

// Yellowf is like fmt.Sprintf with yellow wrapping.
func Yellowf(format string, a ...any) string { return Yellow(fmt.Sprintf(format, a...)) }

// Redf is like fmt.Sprintf with red wrapping.
func Redf(format string, a ...any) string { return Red(fmt.Sprintf(format, a...)) }

// Magentaf is like fmt.Sprintf with magenta wrapping.
func Magentaf(format string, a ...any) string { return Magenta(fmt.Sprintf(format, a...)) }

// Orangef is like fmt.Sprintf with orange wrapping.
func Orangef(format string, a ...any) string { return Orange(fmt.Sprintf(format, a...)) }
