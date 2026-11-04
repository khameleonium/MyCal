package tui

import (
	"bytes"
	"strings"
	"testing"
)

func TestVisualWidth(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"Hello", 5},
		{"Привет", 6}, // 6 Cyrillic characters = 6 visual columns
		{"🗓️", 2},      // Emoji calendar with variation selector
		{"[● Активно]", 11},
		{"\033[31mКрасный\033[0m", 7}, // ANSI stripped
	}

	for _, tt := range tests {
		got := StringVisualWidth(tt.input)
		if got != tt.want {
			t.Errorf("StringVisualWidth(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestTruncateString(t *testing.T) {
	s := "Очень длинное наименование события"
	trunc := TruncateString(s, 10, "…")
	w := StringVisualWidth(trunc)
	if w > 10 {
		t.Errorf("TruncateString width %d exceeds max 10: %q", w, trunc)
	}
	if !strings.HasSuffix(trunc, "…") {
		t.Errorf("expected suffix '…', got %q", trunc)
	}
}

func TestPadRightAndLeft(t *testing.T) {
	s := "Тест"
	right := PadRight(s, 10)
	if StringVisualWidth(right) != 10 {
		t.Errorf("PadRight width = %d, want 10", StringVisualWidth(right))
	}

	left := PadLeft(s, 10)
	if StringVisualWidth(left) != 10 {
		t.Errorf("PadLeft width = %d, want 10", StringVisualWidth(left))
	}
}

func TestScreenDrawBox(t *testing.T) {
	screen := NewScreen(20, 5)
	screen.DrawBox(0, 0, 20, 5, "Заголовок", FgWhite, BorderBright, BgSurface, true)

	var buf bytes.Buffer
	if err := screen.Render(&buf); err != nil {
		t.Fatalf("screen.Render error: %v", err)
	}

	rendered := buf.String()
	if !strings.Contains(rendered, "Заголовок") {
		t.Errorf("expected box title to be rendered, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, string(BoxTL)) {
		t.Errorf("expected box corner %c to be rendered", BoxTL)
	}
}

func TestStatusBadge(t *testing.T) {
	text, fg, bg := StatusBadge("Активно")
	if !strings.Contains(text, "Активно") || fg == "" || bg == "" {
		t.Errorf("invalid StatusBadge for 'Активно': text=%q, fg=%q, bg=%q", text, fg, bg)
	}

	textDone, _, _ := StatusBadge("Завершено")
	if !strings.Contains(textDone, "Завершено") {
		t.Errorf("invalid StatusBadge for 'Завершено': text=%q", textDone)
	}
}

func TestKeyMatching(t *testing.T) {
	kEn := Key{Type: KeyRune, Rune: 'a'}
	if !kEn.Matches('a', 'ф') {
		t.Errorf("expected key 'a' to match ('a', 'ф')")
	}

	kRu := Key{Type: KeyRune, Rune: 'ф'}
	if !kRu.Matches('a', 'ф') {
		t.Errorf("expected key 'ф' to match ('a', 'ф')")
	}

	kUpper := Key{Type: KeyRune, Rune: 'A'}
	if !kUpper.Matches('a', 'ф') {
		t.Errorf("expected uppercase key 'A' to match ('a', 'ф')")
	}
}

func TestWrapText(t *testing.T) {
	text := "Очень длинное наименование события для проверки автоматического переноса строк"
	maxWidth := 20
	lines := WrapText(text, maxWidth)

	if len(lines) < 2 {
		t.Fatalf("expected multiple lines, got %d", len(lines))
	}

	for i, line := range lines {
		w := StringVisualWidth(line)
		if w > maxWidth {
			t.Errorf("line %d exceeds maxWidth %d: got %d (%q)", i, maxWidth, w, line)
		}
	}

	joined := strings.Join(lines, " ")
	for _, word := range strings.Fields(text) {
		if !strings.Contains(joined, word) {
			t.Errorf("word %q lost during wrapping", word)
		}
	}
}

func TestWrapTextLongWord(t *testing.T) {
	longWord := "СупермегадлинноеСловоБезПробеловКотороеНеПомещается"
	maxWidth := 15
	lines := WrapText(longWord, maxWidth)

	if len(lines) < 2 {
		t.Fatalf("expected chunked lines for long word, got %d", len(lines))
	}

	for i, line := range lines {
		w := StringVisualWidth(line)
		if w > maxWidth {
			t.Errorf("chunk %d exceeds maxWidth %d: got %d (%q)", i, maxWidth, w, line)
		}
	}
}

func TestScreenDrawWrappedText(t *testing.T) {
	screen := NewScreen(30, 10)
	text := "Первая строка и продолжение текста на второй и третьей строках"
	linesDrawn := screen.DrawWrappedText(2, 2, 18, 5, text, FgWhite, BgSurface, false, false, false)

	if linesDrawn < 2 {
		t.Errorf("expected at least 2 lines drawn, got %d", linesDrawn)
	}

	// Verify characters are placed in screen cells
	if screen.Cells[2][2].Ch == 0 {
		t.Errorf("expected cell (2,2) to have character")
	}
}
