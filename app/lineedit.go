package app

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// errInterrupted is returned by lineEditor.edit when the user presses Ctrl+C.
var errInterrupted = errors.New("interrupted")

var ansiSequence = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

// readEdited reads one line from a terminal with in-place editing (cursor keys,
// Home/End, Delete, word jumps, paste). prompt is the text already printed
// before the cursor. handled is false when the terminal could not be switched to
// raw mode; the caller then falls back to the terminal's own line input.
func (a *App) readEdited(prompt string) (line string, ok, handled bool) {
	restore, err := makeRaw(os.Stdin.Fd())
	if err != nil {
		a.tty = false
		return "", false, false
	}
	defer restore()

	width := terminalWidth(os.Stdout.Fd())
	if width <= 0 {
		width = 80
	}
	ed := newLineEditor(a.in, os.Stdout, prompt, width)
	line, err = ed.edit()
	switch {
	case err == nil:
		return line, true, true
	case errors.Is(err, errInterrupted):
		restore()
		fmt.Println()
		os.Exit(0)
	}
	a.stdinClosed = true
	return "", false, true
}

// lineEditor is a single-row line editor. It only relies on "\r" and plain
// text output, so it works on any terminal; a line longer than the screen
// scrolls horizontally instead of wrapping.
type lineEditor struct {
	in  *bufio.Reader
	out io.Writer

	width int    // terminal columns
	tail  string // part of the prompt that sits on the input row
	tailW int    // display width of tail

	buf    []rune
	cursor int
	offset int // first visible rune
	drawn  int // columns covered by the previous redraw
	dirty  bool
}

func newLineEditor(in *bufio.Reader, out io.Writer, prompt string, width int) *lineEditor {
	e := &lineEditor{in: in, out: out, width: width}
	e.tail, e.tailW = lastRow(prompt, width)
	e.drawn = e.tailW
	return e
}

// edit reads keys until Enter. It returns io.EOF when input ends (or on Ctrl+D
// with an empty line) and errInterrupted on Ctrl+C.
func (e *lineEditor) edit() (string, error) {
	for {
		r, _, err := e.in.ReadRune()
		if err != nil {
			return "", err
		}
		switch {
		case r == '\r' || r == '\n':
			if r == '\r' && e.in.Buffered() > 0 {
				if b, _ := e.in.Peek(1); len(b) == 1 && b[0] == '\n' {
					_, _ = e.in.Discard(1)
				}
			}
			e.redraw()
			e.write("\r\n")
			return string(e.buf), nil
		case r == 0x03: // Ctrl+C
			return "", errInterrupted
		case r == 0x04: // Ctrl+D
			if len(e.buf) == 0 {
				return "", io.EOF
			}
			e.deleteRange(e.cursor, e.cursor+1)
		case r == 0x01: // Ctrl+A
			e.moveTo(0)
		case r == 0x05: // Ctrl+E
			e.moveTo(len(e.buf))
		case r == 0x02: // Ctrl+B
			e.moveTo(e.cursor - 1)
		case r == 0x06: // Ctrl+F
			e.moveTo(e.cursor + 1)
		case r == 0x08 || r == 0x7f: // Backspace
			e.deleteRange(e.cursor-1, e.cursor)
		case r == 0x0b: // Ctrl+K
			e.deleteRange(e.cursor, len(e.buf))
		case r == 0x15: // Ctrl+U
			e.deleteRange(0, e.cursor)
		case r == 0x17: // Ctrl+W
			e.deleteRange(e.wordStart(), e.cursor)
		case r == 0x1b:
			if err := e.escape(); err != nil {
				return "", err
			}
		case r == '\t':
			e.insert(' ')
		case r == utf8.RuneError || r < 0x20 || (r >= 0x7f && r < 0xa0):
			// other control characters and invalid bytes are ignored
		default:
			e.insert(r)
		}
		if e.dirty && e.in.Buffered() == 0 {
			e.redraw()
		}
	}
}

// escape handles a key sequence that starts with ESC.
func (e *lineEditor) escape() error {
	if e.in.Buffered() == 0 { // a lone Esc key
		return nil
	}
	r, _, err := e.in.ReadRune()
	if err != nil {
		return err
	}
	switch r {
	case '[':
		return e.csi()
	case 'O': // application-mode cursor keys
		if e.in.Buffered() == 0 {
			return nil
		}
		b, err := e.in.ReadByte()
		if err != nil {
			return err
		}
		e.cursorKey(b, "")
	case 'b': // Alt+B
		e.moveTo(e.wordStart())
	case 'f': // Alt+F
		e.moveTo(e.wordEnd())
	case 0x7f: // Alt+Backspace
		e.deleteRange(e.wordStart(), e.cursor)
	}
	return nil
}

// csi reads the rest of an ESC [ sequence and applies it.
func (e *lineEditor) csi() error {
	var params []byte
	for e.in.Buffered() > 0 {
		b, err := e.in.ReadByte()
		if err != nil {
			return err
		}
		if b >= 0x40 && b <= 0x7e {
			e.cursorKey(b, string(params))
			return nil
		}
		params = append(params, b)
	}
	return nil
}

// cursorKey applies a CSI/SS3 final byte; params may carry a key number and a
// modifier ("1;5" = Ctrl).
func (e *lineEditor) cursorKey(final byte, params string) {
	key, mod, _ := strings.Cut(params, ";")
	byWord := mod == "5" || mod == "3"
	switch final {
	case 'D':
		if byWord {
			e.moveTo(e.wordStart())
		} else {
			e.moveTo(e.cursor - 1)
		}
	case 'C':
		if byWord {
			e.moveTo(e.wordEnd())
		} else {
			e.moveTo(e.cursor + 1)
		}
	case 'H':
		e.moveTo(0)
	case 'F':
		e.moveTo(len(e.buf))
	case '~':
		n, _ := strconv.Atoi(key)
		switch n {
		case 1, 7:
			e.moveTo(0)
		case 4, 8:
			e.moveTo(len(e.buf))
		case 3:
			e.deleteRange(e.cursor, e.cursor+1)
		}
	}
}

func (e *lineEditor) insert(r rune) {
	e.buf = append(e.buf, 0)
	copy(e.buf[e.cursor+1:], e.buf[e.cursor:])
	e.buf[e.cursor] = r
	e.cursor++
	e.dirty = true
}

// deleteRange removes buf[from:to], clamped to the line, and puts the cursor at from.
func (e *lineEditor) deleteRange(from, to int) {
	from = max(from, 0)
	to = min(to, len(e.buf))
	if from >= to {
		return
	}
	e.buf = append(e.buf[:from], e.buf[to:]...)
	e.cursor = from
	e.dirty = true
}

func (e *lineEditor) moveTo(pos int) {
	pos = max(0, min(pos, len(e.buf)))
	if pos != e.cursor {
		e.cursor = pos
		e.dirty = true
	}
}

// wordStart returns the start of the word before the cursor.
func (e *lineEditor) wordStart() int {
	i := e.cursor
	for i > 0 && unicode.IsSpace(e.buf[i-1]) {
		i--
	}
	for i > 0 && !unicode.IsSpace(e.buf[i-1]) {
		i--
	}
	return i
}

// wordEnd returns the end of the word at or after the cursor.
func (e *lineEditor) wordEnd() int {
	i := e.cursor
	for i < len(e.buf) && unicode.IsSpace(e.buf[i]) {
		i++
	}
	for i < len(e.buf) && !unicode.IsSpace(e.buf[i]) {
		i++
	}
	return i
}

// redraw repaints the input row: prompt tail, the visible part of the line,
// blanks over whatever was drawn before, then the cursor position.
func (e *lineEditor) redraw() {
	e.dirty = false
	avail := max(e.width-e.tailW-1, 1)

	e.offset = min(e.offset, e.cursor)
	for e.offset < e.cursor && runesWidth(e.buf[e.offset:e.cursor]) > avail {
		e.offset++
	}
	for e.offset > 0 && runesWidth(e.buf[e.offset-1:]) <= avail {
		e.offset--
	}
	end, w := e.offset, 0
	for end < len(e.buf) && w+runeWidth(e.buf[end]) <= avail {
		w += runeWidth(e.buf[end])
		end++
	}

	var sb strings.Builder
	sb.WriteString("\r" + e.tail)
	sb.WriteString(string(e.buf[e.offset:end]))
	if pad := e.drawn - (e.tailW + w); pad > 0 {
		sb.WriteString(strings.Repeat(" ", pad))
	}
	sb.WriteString("\r" + e.tail)
	sb.WriteString(string(e.buf[e.offset:e.cursor]))
	e.write(sb.String())
	e.drawn = e.tailW + w
}

func (e *lineEditor) write(s string) { _, _ = io.WriteString(e.out, s) }

// lastRow returns the part of prompt that lies on the screen row holding the
// cursor (the prompt may contain newlines or wrap) and its display width.
func lastRow(prompt string, width int) (string, int) {
	rs := []rune(ansiSequence.ReplaceAllString(prompt, ""))
	start, col := 0, 0
	for i, r := range rs {
		if r == '\n' {
			start, col = i+1, 0
			continue
		}
		w := runeWidth(r)
		if col+w > width {
			start, col = i, 0
		}
		col += w
	}
	return string(rs[start:]), col
}

func runesWidth(rs []rune) int {
	w := 0
	for _, r := range rs {
		w += runeWidth(r)
	}
	return w
}

// runeWidth returns the number of terminal columns r occupies: 0 for combining
// marks, 2 for East Asian wide characters and emoji, 1 otherwise.
func runeWidth(r rune) int {
	switch {
	case r == 0 || r == 0x200d || unicode.In(r, unicode.Mn, unicode.Me):
		return 0
	case r >= 0x1100 && (r <= 0x115f ||
		(r >= 0x2e80 && r <= 0xa4cf && r != 0x303f) ||
		(r >= 0xac00 && r <= 0xd7a3) ||
		(r >= 0xf900 && r <= 0xfaff) ||
		(r >= 0xfe30 && r <= 0xfe6f) ||
		(r >= 0xff00 && r <= 0xff60) ||
		(r >= 0xffe0 && r <= 0xffe6) ||
		(r >= 0x1f300 && r <= 0x1f64f) ||
		(r >= 0x1f900 && r <= 0x1f9ff) ||
		(r >= 0x20000 && r <= 0x3fffd)):
		return 2
	}
	return 1
}
