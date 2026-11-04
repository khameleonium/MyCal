package tui

import (
	"strings"
	"unicode/utf8"
)

// ANSI Styling Constants
const (
	Reset     = "\033[0m"
	Bold      = "\033[1m"
	Dim       = "\033[2m"
	Italic    = "\033[3m"
	Underline = "\033[4m"
	Inverse   = "\033[7m"

	// Background colors (24-bit TrueColor)
	BgPrimary   = "\033[48;2;15;20;28m"  // #0f141c Main app backdrop
	BgSurface   = "\033[48;2;22;29;39m"  // #161d27 Card / Panel background
	BgPanelAlt  = "\033[48;2;19;25;35m"  // #131923 Header / Alt panel
	BgCard      = "\033[48;2;27;35;48m"  // #1b2330 Inner section card
	BgHighlight = "\033[48;2;40;51;70m"  // #283346 Row highlight / hover
	BgSelected  = "\033[48;2;42;59;92m"  // #2a3b5c Focused selection
	BgModal     = "\033[48;2;24;32;44m"  // #18202c Modal window body
	BgModalHdr  = "\033[48;2;30;40;56m"  // #1e2838 Modal header
	BgInput     = "\033[48;2;15;20;28m"  // #0f141c Input field background

	// Foreground colors (24-bit TrueColor)
	FgMain    = "\033[38;2;226;232;240m" // #e2e8f0 Primary text
	FgMuted   = "\033[38;2;130;149;176m" // #8295b0 Muted text / labels
	FgDim     = "\033[38;2;84;102;130m"  // #546682 Dim / borders
	FgWhite   = "\033[38;2;255;255;255m" // Pure white
	FgBlue    = "\033[38;2;96;165;250m"  // #60a5fa Accent blue
	FgCyan    = "\033[38;2;56;189;248m"  // #38bdf8 Accent cyan
	FgGreen   = "\033[38;2;74;222;128m"  // #4ade80 Accent green
	FgYellow  = "\033[38;2;250;204;21m"  // #facc15 Accent yellow
	FgOrange  = "\033[38;2;251;146;60m"  // #fb923c Accent orange
	FgMagenta = "\033[38;2;192;132;252m" // #c084fc Accent magenta
	FgRed     = "\033[38;2;248;113;113m" // #f87171 Accent red

	// Border colors
	BorderDim    = "\033[38;2;38;51;69m"   // #263345 Subtle border
	BorderBright = "\033[38;2;61;81;112m"  // #3d5170 Standard border
	BorderFocus  = "\033[38;2;79;135;226m" // #4f87e2 Focused panel border
)

// Box drawing runes
const (
	BoxTL = '╭'
	BoxTR = '╮'
	BoxBL = '╰'
	BoxBR = '╯'
	BoxH  = '─'
	BoxV  = '│'
	BoxLT = '├'
	BoxRT = '┤'
	BoxTT = '┬'
	BoxBT = '┴'
	BoxX  = '┼'
)

// StatusBadge returns the formatted status badge string with appropriate colors.
func StatusBadge(status string) (text string, fg string, bg string) {
	s := strings.TrimSpace(status)
	switch {
	case strings.EqualFold(s, "Активно") || strings.EqualFold(s, "Active"):
		return "● Активно", FgBlue, "\033[48;2;25;45;75m"
	case strings.EqualFold(s, "Завершено") || strings.EqualFold(s, "Completed") || strings.EqualFold(s, "Done"):
		return "✓ Завершено", FgGreen, "\033[48;2;20;50;35m"
	case strings.EqualFold(s, "Запланировано") || strings.EqualFold(s, "Planned"):
		return "⏱ Запланировано", FgCyan, "\033[48;2;20;50;60m"
	case strings.EqualFold(s, "Отменено") || strings.EqualFold(s, "Cancelled"):
		return "✗ Отменено", FgRed, "\033[48;2;60;25;25m"
	case strings.EqualFold(s, "В процессе") || strings.EqualFold(s, "In Progress"):
		return "⏳ В процессе", FgYellow, "\033[48;2;60;50;20m"
	case strings.EqualFold(s, "Пропущено") || strings.EqualFold(s, "Missed"):
		return "⊘ Пропущено", FgOrange, "\033[48;2;60;40;20m"
	case s == "":
		return "(без статуса)", FgDim, BgCard
	default:
		return s, FgMain, BgCard
	}
}

// RuneVisualWidth returns the number of terminal columns required to display rune r.
func RuneVisualWidth(r rune) int {
	if r == 0 || r == '\uFE0F' || (r >= 0x200B && r <= 0x200D) {
		return 0
	}
	// Wide characters: emojis, symbols, and CJK ideographs
	if (r >= 0x1F300 && r <= 0x1FAFF) ||
		(r >= 0x2600 && r <= 0x27BF) ||
		(r >= 0x2E80 && r <= 0x9FFF) ||
		(r >= 0xAC00 && r <= 0xD7AF) ||
		(r >= 0xF900 && r <= 0xFAFF) ||
		(r >= 0xFE10 && r <= 0xFE19) ||
		(r >= 0xFE30 && r <= 0xFE6F) ||
		(r >= 0xFF00 && r <= 0xFF60) ||
		(r >= 0xFFE0 && r <= 0xFFE6) {
		return 2
	}
	return 1
}

// StringVisualWidth returns the display width of a string in terminal cells (stripping ANSI).
func StringVisualWidth(s string) int {
	w := 0
	inEsc := false
	for _, r := range s {
		if r == '\033' {
			inEsc = true
			continue
		}
		if inEsc {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == 'm' {
				inEsc = false
			}
			continue
		}
		w += RuneVisualWidth(r)
	}
	return w
}

// TruncateString truncates string s so its visual width does not exceed maxWidth.
// If truncated, tail (e.g. "…") is appended.
func TruncateString(s string, maxWidth int, tail string) string {
	if maxWidth <= 0 {
		return ""
	}
	currW := StringVisualWidth(s)
	if currW <= maxWidth {
		return s
	}

	tailW := StringVisualWidth(tail)
	targetW := maxWidth - tailW
	if targetW < 0 {
		targetW = 0
	}

	var sb strings.Builder
	w := 0
	for _, r := range s {
		rw := RuneVisualWidth(r)
		if w+rw > targetW {
			break
		}
		sb.WriteRune(r)
		w += rw
	}
	sb.WriteString(tail)
	return sb.String()
}

// PadRight pads s with spaces until it reaches targetWidth visual columns.
func PadRight(s string, targetWidth int) string {
	w := StringVisualWidth(s)
	if w >= targetWidth {
		return s
	}
	return s + strings.Repeat(" ", targetWidth-w)
}

// PadLeft pads s with spaces on the left until it reaches targetWidth visual columns.
func PadLeft(s string, targetWidth int) string {
	w := StringVisualWidth(s)
	if w >= targetWidth {
		return s
	}
	return strings.Repeat(" ", targetWidth-w) + s
}

// CenterString centers s within targetWidth visual columns.
func CenterString(s string, targetWidth int) string {
	w := StringVisualWidth(s)
	if w >= targetWidth {
		return s
	}
	left := (targetWidth - w) / 2
	right := targetWidth - w - left
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", right)
}

// CleanString strips control characters and newlines for single-line display.
func CleanString(s string) string {
	var sb strings.Builder
	for _, r := range s {
		if r == '\r' || r == '\n' || r == '\t' {
			sb.WriteRune(' ')
		} else if !utf8.ValidRune(r) || r < 32 {
			continue
		} else {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// WrapText wraps text into lines such that each line has visual width <= maxWidth.
// It wraps on word boundaries (spaces), or breaks long words if a single word exceeds maxWidth.
func WrapText(text string, maxWidth int) []string {
	if maxWidth <= 0 {
		return nil
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	var lines []string
	rawLines := strings.Split(text, "\n")

	for _, rawLine := range rawLines {
		rawLine = strings.TrimRight(rawLine, "\r\n")
		words := strings.Fields(rawLine)
		if len(words) == 0 {
			lines = append(lines, "")
			continue
		}

		currentLine := ""
		currentWidth := 0

		for _, word := range words {
			wWidth := StringVisualWidth(word)

			if currentLine == "" {
				if wWidth <= maxWidth {
					currentLine = word
					currentWidth = wWidth
				} else {
					// Word itself is longer than maxWidth: chunk it
					chunks := chunkLongWord(word, maxWidth)
					for i, chk := range chunks {
						if i < len(chunks)-1 {
							lines = append(lines, chk)
						} else {
							currentLine = chk
							currentWidth = StringVisualWidth(chk)
						}
					}
				}
			} else {
				// Try adding word to currentLine with a space
				if currentWidth+1+wWidth <= maxWidth {
					currentLine += " " + word
					currentWidth += 1 + wWidth
				} else {
					lines = append(lines, currentLine)
					if wWidth <= maxWidth {
						currentLine = word
						currentWidth = wWidth
					} else {
						chunks := chunkLongWord(word, maxWidth)
						for i, chk := range chunks {
							if i < len(chunks)-1 {
								lines = append(lines, chk)
							} else {
								currentLine = chk
								currentWidth = StringVisualWidth(chk)
							}
						}
					}
				}
			}
		}

		if currentLine != "" {
			lines = append(lines, currentLine)
		}
	}

	return lines
}

func chunkLongWord(word string, maxWidth int) []string {
	var chunks []string
	var sb strings.Builder
	w := 0
	for _, r := range word {
		rw := RuneVisualWidth(r)
		if w+rw > maxWidth {
			chunks = append(chunks, sb.String())
			sb.Reset()
			w = 0
		}
		sb.WriteRune(r)
		w += rw
	}
	if sb.Len() > 0 {
		chunks = append(chunks, sb.String())
	}
	return chunks
}
