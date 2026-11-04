package tui

import (
	"bytes"
	"io"
)

// Cell represents one terminal character cell.
type Cell struct {
	Ch        rune
	Fg        string
	Bg        string
	Bold      bool
	Dim       bool
	Underline bool
	Wide      bool // true if this cell is the second half of a 2-width rune
}

// Screen represents an in-memory 2D grid buffer.
type Screen struct {
	Width  int
	Height int
	Cells  [][]Cell
}

// NewScreen creates a new Screen with the specified width and height.
func NewScreen(width, height int) *Screen {
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 24
	}

	cells := make([][]Cell, height)
	for y := 0; y < height; y++ {
		cells[y] = make([]Cell, width)
		for x := 0; x < width; x++ {
			cells[y][x] = Cell{
				Ch: ' ',
				Fg: FgMain,
				Bg: BgPrimary,
			}
		}
	}

	return &Screen{
		Width:  width,
		Height: height,
		Cells:  cells,
	}
}

// Clear fills the entire screen with blank spaces of the given background color.
func (s *Screen) Clear(bg string) {
	for y := 0; y < s.Height; y++ {
		for x := 0; x < s.Width; x++ {
			s.Cells[y][x] = Cell{
				Ch: ' ',
				Fg: FgMain,
				Bg: bg,
			}
		}
	}
}

// SetCell sets a single character cell at (x, y).
func (s *Screen) SetCell(x, y int, ch rune, fg, bg string, bold, dim, underline bool) {
	if x < 0 || x >= s.Width || y < 0 || y >= s.Height {
		return
	}
	s.Cells[y][x] = Cell{
		Ch:        ch,
		Fg:        fg,
		Bg:        bg,
		Bold:      bold,
		Dim:       dim,
		Underline: underline,
		Wide:      false,
	}
}

// DrawText prints a string starting at (x, y) with the specified styles.
// Handles multi-byte runes and wide (2-column) emojis/symbols cleanly.
func (s *Screen) DrawText(x, y int, text, fg, bg string, maxWidth int, bold, dim, underline bool) int {
	if y < 0 || y >= s.Height || x >= s.Width {
		return 0
	}

	cx := x
	limit := s.Width
	if maxWidth > 0 && x+maxWidth < limit {
		limit = x + maxWidth
	}

	inEsc := false
	for _, r := range text {
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

		if cx >= limit {
			break
		}

		rw := RuneVisualWidth(r)
		if rw == 0 {
			continue
		}

		if cx < 0 {
			cx += rw
			continue
		}

		if rw == 2 && cx+1 >= limit {
			// Not enough space for wide character, break
			break
		}

		s.Cells[y][cx] = Cell{
			Ch:        r,
			Fg:        fg,
			Bg:        bg,
			Bold:      bold,
			Dim:       dim,
			Underline: underline,
			Wide:      false,
		}
		cx++

		if rw == 2 {
			s.Cells[y][cx] = Cell{
				Ch:        ' ',
				Fg:        fg,
				Bg:        bg,
				Bold:      bold,
				Dim:       dim,
				Underline: underline,
				Wide:      true,
			}
			cx++
		}
	}

	return cx - x
}

// DrawWrappedText wraps text to maxWidth and draws up to maxHeight lines starting at (x, y).
// It returns the number of lines drawn.
func (s *Screen) DrawWrappedText(x, y, maxWidth, maxHeight int, text, fg, bg string, bold, dim, underline bool) int {
	if maxWidth <= 0 || maxHeight <= 0 || y >= s.Height {
		return 0
	}
	lines := WrapText(text, maxWidth)
	drawn := 0
	for i, line := range lines {
		if i >= maxHeight || y+i >= s.Height {
			break
		}
		s.DrawText(x, y+i, line, fg, bg, maxWidth, bold, dim, underline)
		drawn++
	}
	return drawn
}

// DrawHLine draws a horizontal line from x1 to x2 (inclusive) on row y.
func (s *Screen) DrawHLine(x1, x2, y int, ch rune, fg, bg string) {
	if y < 0 || y >= s.Height {
		return
	}
	if x1 > x2 {
		x1, x2 = x2, x1
	}
	if x1 < 0 {
		x1 = 0
	}
	if x2 >= s.Width {
		x2 = s.Width - 1
	}
	for x := x1; x <= x2; x++ {
		s.Cells[y][x] = Cell{Ch: ch, Fg: fg, Bg: bg}
	}
}

// DrawVLine draws a vertical line from y1 to y2 (inclusive) on column x.
func (s *Screen) DrawVLine(x, y1, y2 int, ch rune, fg, bg string) {
	if x < 0 || x >= s.Width {
		return
	}
	if y1 > y2 {
		y1, y2 = y2, y1
	}
	if y1 < 0 {
		y1 = 0
	}
	if y2 >= s.Height {
		y2 = s.Height - 1
	}
	for y := y1; y <= y2; y++ {
		s.Cells[y][x] = Cell{Ch: ch, Fg: fg, Bg: bg}
	}
}

// Fill fills a rectangular area with character ch and background bg.
func (s *Screen) Fill(x, y, w, h int, ch rune, fg, bg string) {
	for row := y; row < y+h && row < s.Height; row++ {
		if row < 0 {
			continue
		}
		for col := x; col < x+w && col < s.Width; col++ {
			if col < 0 {
				continue
			}
			s.Cells[row][col] = Cell{Ch: ch, Fg: fg, Bg: bg}
		}
	}
}

// DrawBox renders a stylish rounded box with an optional title.
func (s *Screen) DrawBox(x, y, w, h int, title, titleFg, borderFg, bg string, boldBorder bool) {
	if w <= 1 || h <= 1 {
		return
	}
	x2 := x + w - 1
	y2 := y + h - 1

	// Fill background
	s.Fill(x, y, w, h, ' ', FgMain, bg)

	// Corners
	s.SetCell(x, y, BoxTL, borderFg, bg, boldBorder, false, false)
	s.SetCell(x2, y, BoxTR, borderFg, bg, boldBorder, false, false)
	s.SetCell(x, y2, BoxBL, borderFg, bg, boldBorder, false, false)
	s.SetCell(x2, y2, BoxBR, borderFg, bg, boldBorder, false, false)

	// Horizontal borders
	s.DrawHLine(x+1, x2-1, y, BoxH, borderFg, bg)
	s.DrawHLine(x+1, x2-1, y2, BoxH, borderFg, bg)

	// Vertical borders
	s.DrawVLine(x, y+1, y2-1, BoxV, borderFg, bg)
	s.DrawVLine(x2, y+1, y2-1, BoxV, borderFg, bg)

	// Title
	if title != "" && w > 6 {
		cleanTitle := " " + TruncateString(title, w-6, "…") + " "
		s.DrawText(x+2, y, cleanTitle, titleFg, bg, w-4, true, false, false)
	}
}

// Render writes the full screen buffer to out in one single atomic write.
func (s *Screen) Render(out io.Writer) error {
	var buf bytes.Buffer
	// Move cursor to top-left (1,1)
	buf.WriteString("\033[H")

	currFg := ""
	currBg := ""
	currBold := false
	currDim := false
	currUnderline := false

	resetStyles := func() {
		buf.WriteString(Reset)
		currFg = ""
		currBg = ""
		currBold = false
		currDim = false
		currUnderline = false
	}

	for y := 0; y < s.Height; y++ {
		for x := 0; x < s.Width; x++ {
			c := s.Cells[y][x]
			if c.Wide {
				continue
			}

			// Style differences check
			needReset := false
			if (currBold && !c.Bold) || (currDim && !c.Dim) || (currUnderline && !c.Underline) {
				needReset = true
			}

			if needReset {
				resetStyles()
			}

			if c.Fg != currFg && c.Fg != "" {
				buf.WriteString(c.Fg)
				currFg = c.Fg
			}
			if c.Bg != currBg && c.Bg != "" {
				buf.WriteString(c.Bg)
				currBg = c.Bg
			}
			if c.Bold && !currBold {
				buf.WriteString(Bold)
				currBold = true
			}
			if c.Dim && !currDim {
				buf.WriteString(Dim)
				currDim = true
			}
			if c.Underline && !currUnderline {
				buf.WriteString(Underline)
				currUnderline = true
			}

			if c.Ch == 0 {
				buf.WriteByte(' ')
			} else {
				buf.WriteRune(c.Ch)
			}
		}
		if y < s.Height-1 {
			resetStyles()
			buf.WriteString("\r\n")
		}
	}
	resetStyles()

	_, err := out.Write(buf.Bytes())
	return err
}
