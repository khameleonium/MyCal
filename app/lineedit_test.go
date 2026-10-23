package app

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

const (
	keyLeft  = "\x1b[D"
	keyRight = "\x1b[C"
	keyHome  = "\x1b[H"
	keyEnd   = "\x1b[F"
	keyDel   = "\x1b[3~"
)

func runEditor(t *testing.T, keys string, width int) (string, string, error) {
	t.Helper()
	var out bytes.Buffer
	e := newLineEditor(bufio.NewReader(strings.NewReader(keys)), &out, "> ", width)
	got, err := e.edit()
	return got, out.String(), err
}

func TestLineEditorKeys(t *testing.T) {
	tests := []struct {
		name string
		keys string
		want string
	}{
		{"plain", "abc\r", "abc"},
		{"cyrillic", "привет\r", "привет"},
		{"backspace 0x7f", "abc\x7f\r", "ab"},
		{"backspace 0x08", "abc\x08\r", "ab"},
		{"backspace on empty", "\x7f\x7f\r", ""},
		{"left then insert", "abd" + keyLeft + "c\r", "abcd"},
		{"left twice then delete key", "abcd" + keyLeft + keyLeft + keyDel + "\r", "abd"},
		{"left twice then backspace", "abcd" + keyLeft + keyLeft + "\x7f\r", "acd"},
		{"home", "bc" + keyHome + "a\r", "abc"},
		{"end", "ab" + keyHome + keyEnd + "c\r", "abc"},
		{"right past end is clamped", "ab" + keyRight + keyRight + "c\r", "abc"},
		{"left past start is clamped", "b" + keyLeft + keyLeft + "a\r", "ab"},
		{"home via tilde 1", "b\x1b[1~a\r", "ab"},
		{"end via tilde 4", "ab\x1b[1~\x1b[4~c\r", "abc"},
		{"ss3 arrows", "ac\x1bODb\r", "abc"},
		{"ctrl+a ctrl+e", "bc\x01a\x05d\r", "abcd"},
		{"ctrl+b ctrl+f", "ac\x02b\x06d\r", "abcd"},
		{"ctrl+k kills to end", "abcd" + keyLeft + keyLeft + "\x0b\r", "ab"},
		{"ctrl+u kills to start", "abcd" + keyLeft + "\x15\r", "d"},
		{"ctrl+w deletes word", "one two three\x17\r", "one two "},
		{"ctrl+w skips trailing spaces", "one two  \x17\r", "one "},
		{"ctrl+left word jump", "one two\x1b[1;5Dx\r", "one xtwo"},
		{"ctrl+right word jump", "one two" + keyHome + "\x1b[1;5Cx\r", "onex two"},
		{"alt+backspace", "one two\x1b\x7f\r", "one "},
		{"ctrl+d deletes under cursor", "abc" + keyLeft + "\x04\r", "ab"},
		{"unknown sequences ignored", "a\x1b[5~\x1b[Ab\x1b[1;2Pc\r", "abc"},
		{"lone esc ignored", "a\x1b", ""},
		{"tab becomes space", "a\tb\r", "a b"},
		{"control chars ignored", "a\x00\x07b\r", "ab"},
		{"crlf swallowed", "ab\r\n", "ab"},
		{"lf submits", "ab\n", "ab"},
		{"emoji", "a😀b" + keyLeft + keyLeft + "x\r", "ax😀b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _, err := runEditor(t, tt.keys, 80)
			if tt.name == "lone esc ignored" {
				if !errors.Is(err, io.EOF) {
					t.Fatalf("err = %v, want EOF", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("edit: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLineEditorCrlfDoesNotLeakIntoNextLine(t *testing.T) {
	in := bufio.NewReader(strings.NewReader("first\r\nsecond\r\n"))
	var out bytes.Buffer
	for _, want := range []string{"first", "second"} {
		e := newLineEditor(in, &out, "> ", 80)
		got, err := e.edit()
		if err != nil || got != want {
			t.Fatalf("got %q, %v; want %q", got, err, want)
		}
	}
}

func TestLineEditorPaste(t *testing.T) {
	text := strings.Repeat("длинный вставленный текст ", 20)
	got, _, err := runEditor(t, text+keyLeft+keyLeft+"!\r", 40)
	if err != nil {
		t.Fatal(err)
	}
	rs := []rune(text)
	want := string(rs[:len(rs)-2]) + "!" + string(rs[len(rs)-2:])
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestLineEditorInterruptAndEOF(t *testing.T) {
	if _, _, err := runEditor(t, "abc\x03", 80); !errors.Is(err, errInterrupted) {
		t.Errorf("Ctrl+C: err = %v, want errInterrupted", err)
	}
	if _, _, err := runEditor(t, "\x04", 80); !errors.Is(err, io.EOF) {
		t.Errorf("Ctrl+D on empty line: err = %v, want EOF", err)
	}
	if _, _, err := runEditor(t, "abc", 80); !errors.Is(err, io.EOF) {
		t.Errorf("closed input: err = %v, want EOF", err)
	}
}

// The output must never contain a newline before Enter, and a line wider than
// the screen must stay on one row.
func TestLineEditorStaysOnOneRow(t *testing.T) {
	const width = 20
	_, out, err := runEditor(t, strings.Repeat("ж", 100)+"\r", width)
	if err != nil {
		t.Fatal(err)
	}
	body := strings.TrimSuffix(out, "\r\n")
	if strings.ContainsAny(body, "\n") {
		t.Fatalf("newline printed before Enter: %q", body)
	}
	for _, row := range strings.Split(body, "\r") {
		if n := len([]rune(row)); n >= width {
			t.Errorf("row %q has %d columns, want < %d", row, n, width)
		}
	}
}

func TestLineEditorRedrawErasesDeletedText(t *testing.T) {
	var out bytes.Buffer
	e := newLineEditor(bufio.NewReader(strings.NewReader("")), &out, "> ", 80)
	for _, r := range "hello" {
		e.insert(r)
	}
	e.redraw()
	out.Reset()
	e.deleteRange(0, 5)
	e.redraw()
	want := "\r> " + strings.Repeat(" ", 5) + "\r> "
	if out.String() != want {
		t.Errorf("redraw = %q, want %q", out.String(), want)
	}
}

func TestLastRow(t *testing.T) {
	tests := []struct {
		prompt string
		width  int
		tail   string
		w      int
	}{
		{"> ", 80, "> ", 2},
		{"\x1b[33mВопрос?\x1b[0m (Да/Нет) > ", 80, "Вопрос? (Да/Нет) > ", 19},
		{"title\n> ", 80, "> ", 2},
		{"abcdefghij", 4, "ij", 2},
		{"", 80, "", 0},
	}
	for _, tt := range tests {
		tail, w := lastRow(tt.prompt, tt.width)
		if tail != tt.tail || w != tt.w {
			t.Errorf("lastRow(%q, %d) = %q, %d; want %q, %d", tt.prompt, tt.width, tail, w, tt.tail, tt.w)
		}
	}
}

func TestRuneWidth(t *testing.T) {
	tests := []struct {
		r    rune
		want int
	}{
		{'a', 1}, {'ж', 1}, {'世', 2}, {'😀', 2}, {'́', 0},
	}
	for _, tt := range tests {
		if got := runeWidth(tt.r); got != tt.want {
			t.Errorf("runeWidth(%q) = %d, want %d", tt.r, got, tt.want)
		}
	}
}
