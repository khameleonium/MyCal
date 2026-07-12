package color

import "fmt"

const (
	reset   = "\033[0m"
	green   = "\033[32m"
	yellow  = "\033[33m"
	red     = "\033[31m"
	magenta = "\033[35m"
	orange  = "\033[38;5;208m"
)

// Green wraps s in green ANSI codes.
func Green(s string) string {
	return green + s + reset
}

// Yellow wraps s in yellow ANSI codes.
func Yellow(s string) string {
	return yellow + s + reset
}

// Red wraps s in red ANSI codes.
func Red(s string) string {
	return red + s + reset
}

// Magenta wraps s in magenta ANSI codes.
func Magenta(s string) string {
	return magenta + s + reset
}

// Orange wraps s in orange ANSI codes (256-color, may not work in very old terminals).
func Orange(s string) string {
	return orange + s + reset
}

// Greenf is like fmt.Sprintf with green wrapping.
func Greenf(format string, a ...any) string {
	return Green(fmt.Sprintf(format, a...))
}

// Yellowf is like fmt.Sprintf with yellow wrapping.
func Yellowf(format string, a ...any) string {
	return Yellow(fmt.Sprintf(format, a...))
}

// Redf is like fmt.Sprintf with red wrapping.
func Redf(format string, a ...any) string {
	return Red(fmt.Sprintf(format, a...))
}

// Magentaf is like fmt.Sprintf with magenta wrapping.
func Magentaf(format string, a ...any) string {
	return Magenta(fmt.Sprintf(format, a...))
}

// Orangef is like fmt.Sprintf with orange wrapping.
func Orangef(format string, a ...any) string {
	return Orange(fmt.Sprintf(format, a...))
}
