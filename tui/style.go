package tui

import (
	"strings"
	"unicode/utf8"
)

// ANSI Styling Constants (Formatting)
const (
	Reset     = "\033[0m"
	Bold      = "\033[1m"
	Dim       = "\033[2m"
	Italic    = "\033[3m"
	Underline = "\033[4m"
	Inverse   = "\033[7m"
)

// Active Theme Colors (24-bit TrueColor)
var (
	// Background colors
	BgPrimary   = "\033[48;2;15;20;28m" // #0f141c Main app backdrop
	BgSurface   = "\033[48;2;22;29;39m" // #161d27 Card / Panel background
	BgPanelAlt  = "\033[48;2;19;25;35m" // #131923 Header / Alt panel
	BgCard      = "\033[48;2;27;35;48m" // #1b2330 Inner section card
	BgHighlight = "\033[48;2;40;51;70m" // #283346 Row highlight / hover
	BgSelected  = "\033[48;2;42;59;92m" // #2a3b5c Focused selection
	BgModal     = "\033[48;2;24;32;44m" // #18202c Modal window body
	BgModalHdr  = "\033[48;2;30;40;56m" // #1e2838 Modal header
	BgInput     = "\033[48;2;15;20;28m" // #0f141c Input field background

	// Foreground colors
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

	// Status badge backgrounds
	StatusBgActive = "\033[48;2;25;45;75m"
	StatusBgDone   = "\033[48;2;20;50;35m"
	StatusBgProg   = "\033[48;2;60;50;20m"
	StatusBgPlan   = "\033[48;2;20;50;60m"
	StatusBgMissed = "\033[48;2;60;40;20m"
	StatusBgCancel = "\033[48;2;60;25;25m"
)

// ThemeDef defines a complete palette for TUI styling.
type ThemeDef struct {
	ID          string
	Name        string
	Description string

	BgPrimary   string
	BgSurface   string
	BgPanelAlt  string
	BgCard      string
	BgHighlight string
	BgSelected  string
	BgModal     string
	BgModalHdr  string
	BgInput     string

	FgMain      string
	FgMuted     string
	FgDim       string
	FgWhite     string
	FgBlue      string
	FgCyan      string
	FgGreen     string
	FgYellow    string
	FgOrange    string
	FgMagenta   string
	FgRed       string

	BorderDim    string
	BorderBright string
	BorderFocus  string

	StatusBgActive string
	StatusBgDone   string
	StatusBgProg   string
	StatusBgPlan   string
	StatusBgMissed string
	StatusBgCancel string
}

func hexFg(hex string) string {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) == 6 {
		r := hexToInt(hex[0:2])
		g := hexToInt(hex[2:4])
		b := hexToInt(hex[4:6])
		return "\033[38;2;" + itoa(r) + ";" + itoa(g) + ";" + itoa(b) + "m"
	}
	return "\033[38;2;226;232;240m"
}

func hexBg(hex string) string {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) == 6 {
		r := hexToInt(hex[0:2])
		g := hexToInt(hex[2:4])
		b := hexToInt(hex[4:6])
		return "\033[48;2;" + itoa(r) + ";" + itoa(g) + ";" + itoa(b) + "m"
	}
	return "\033[48;2;15;20;28m"
}

func hexToInt(s string) int {
	res := 0
	for _, c := range s {
		res *= 16
		switch {
		case c >= '0' && c <= '9':
			res += int(c - '0')
		case c >= 'a' && c <= 'f':
			res += int(c - 'a' + 10)
		case c >= 'A' && c <= 'F':
			res += int(c - 'A' + 10)
		}
	}
	return res
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var b [10]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
}

var (
	activeThemeID = "tokyo"

	availableThemes = []ThemeDef{
		{
			ID:          "tokyo",
			Name:        "Tokyo Night",
			Description: "Тёмный неоновый океан (по умолч.)",
			BgPrimary:   hexBg("#0f141c"),
			BgSurface:   hexBg("#161d27"),
			BgPanelAlt:  hexBg("#131923"),
			BgCard:      hexBg("#1b2330"),
			BgHighlight: hexBg("#283346"),
			BgSelected:  hexBg("#2a3b5c"),
			BgModal:     hexBg("#18202c"),
			BgModalHdr:  hexBg("#1e2838"),
			BgInput:     hexBg("#0f141c"),
			FgMain:      hexFg("#e2e8f0"),
			FgMuted:     hexFg("#8295b0"),
			FgDim:       hexFg("#546682"),
			FgWhite:     hexFg("#ffffff"),
			FgBlue:      hexFg("#60a5fa"),
			FgCyan:      hexFg("#38bdf8"),
			FgGreen:     hexFg("#4ade80"),
			FgYellow:    hexFg("#facc15"),
			FgOrange:    hexFg("#fb923c"),
			FgMagenta:   hexFg("#c084fc"),
			FgRed:       hexFg("#f87171"),
			BorderDim:    hexFg("#263345"),
			BorderBright: hexFg("#3d5170"),
			BorderFocus:  hexFg("#4f87e2"),
			StatusBgActive: hexBg("#192d4b"),
			StatusBgDone:   hexBg("#143223"),
			StatusBgProg:   hexBg("#3c3214"),
			StatusBgPlan:   hexBg("#14323c"),
			StatusBgMissed: hexBg("#3c2814"),
			StatusBgCancel: hexBg("#3c1919"),
		},
		{
			ID:          "catppuccin",
			Name:        "Catppuccin Mocha",
			Description: "Тёплый пастельный фиолетово-персиковый стиль",
			BgPrimary:   hexBg("#181825"),
			BgSurface:   hexBg("#1e1e2e"),
			BgPanelAlt:  hexBg("#11111b"),
			BgCard:      hexBg("#313244"),
			BgHighlight: hexBg("#45475a"),
			BgSelected:  hexBg("#585b70"),
			BgModal:     hexBg("#1e1e2e"),
			BgModalHdr:  hexBg("#313244"),
			BgInput:     hexBg("#11111b"),
			FgMain:      hexFg("#cdd6f4"),
			FgMuted:     hexFg("#a6adc8"),
			FgDim:       hexFg("#6c7086"),
			FgWhite:     hexFg("#ffffff"),
			FgBlue:      hexFg("#89b4fa"),
			FgCyan:      hexFg("#89dceb"),
			FgGreen:     hexFg("#a6e3a1"),
			FgYellow:    hexFg("#f9e2af"),
			FgOrange:    hexFg("#fab387"),
			FgMagenta:   hexFg("#cba6f7"),
			FgRed:       hexFg("#f38ba8"),
			BorderDim:    hexFg("#313244"),
			BorderBright: hexFg("#585b70"),
			BorderFocus:  hexFg("#cba6f7"),
			StatusBgActive: hexBg("#202a45"),
			StatusBgDone:   hexBg("#223829"),
			StatusBgProg:   hexBg("#3f351e"),
			StatusBgPlan:   hexBg("#1f343c"),
			StatusBgMissed: hexBg("#3d281e"),
			StatusBgCancel: hexBg("#3b1c25"),
		},
		{
			ID:          "nord",
			Name:        "Nord Frost",
			Description: "Холодный арктический стиль с пастельными акцентами",
			BgPrimary:   hexBg("#242933"),
			BgSurface:   hexBg("#2e3440"),
			BgPanelAlt:  hexBg("#242933"),
			BgCard:      hexBg("#3b4252"),
			BgHighlight: hexBg("#434c5e"),
			BgSelected:  hexBg("#4c566a"),
			BgModal:     hexBg("#2e3440"),
			BgModalHdr:  hexBg("#3b4252"),
			BgInput:     hexBg("#242933"),
			FgMain:      hexFg("#eceff4"),
			FgMuted:     hexFg("#d8dee9"),
			FgDim:       hexFg("#7f8ca3"),
			FgWhite:     hexFg("#ffffff"),
			FgBlue:      hexFg("#88c0d0"),
			FgCyan:      hexFg("#8fbcbb"),
			FgGreen:     hexFg("#a3be8c"),
			FgYellow:    hexFg("#ebcb8b"),
			FgOrange:    hexFg("#d08770"),
			FgMagenta:   hexFg("#b48ead"),
			FgRed:       hexFg("#bf616a"),
			BorderDim:    hexFg("#3b4252"),
			BorderBright: hexFg("#4c566a"),
			BorderFocus:  hexFg("#88c0d0"),
			StatusBgActive: hexBg("#2b3d4f"),
			StatusBgDone:   hexBg("#2c4232"),
			StatusBgProg:   hexBg("#4c4228"),
			StatusBgPlan:   hexBg("#264042"),
			StatusBgMissed: hexBg("#472b22"),
			StatusBgCancel: hexBg("#422024"),
		},
		{
			ID:          "dracula",
			Name:        "Dracula",
			Description: "Тёмно-фиолетовый вампирский с неоновым розовым",
			BgPrimary:   hexBg("#1e1f29"),
			BgSurface:   hexBg("#282a36"),
			BgPanelAlt:  hexBg("#191a21"),
			BgCard:      hexBg("#343746"),
			BgHighlight: hexBg("#44475a"),
			BgSelected:  hexBg("#44475a"),
			BgModal:     hexBg("#282a36"),
			BgModalHdr:  hexBg("#343746"),
			BgInput:     hexBg("#191a21"),
			FgMain:      hexFg("#f8f8f2"),
			FgMuted:     hexFg("#e2e2dc"),
			FgDim:       hexFg("#6272a4"),
			FgWhite:     hexFg("#ffffff"),
			FgBlue:      hexFg("#8be9fd"),
			FgCyan:      hexFg("#8be9fd"),
			FgGreen:     hexFg("#50fa7b"),
			FgYellow:    hexFg("#f1fa8c"),
			FgOrange:    hexFg("#ffb86c"),
			FgMagenta:   hexFg("#bd93f9"),
			FgRed:       hexFg("#ff5555"),
			BorderDim:    hexFg("#343746"),
			BorderBright: hexFg("#6272a4"),
			BorderFocus:  hexFg("#ff79c6"),
			StatusBgActive: hexBg("#2a3250"),
			StatusBgDone:   hexBg("#1e402c"),
			StatusBgProg:   hexBg("#4a4720"),
			StatusBgPlan:   hexBg("#1b4249"),
			StatusBgMissed: hexBg("#4a3019"),
			StatusBgCancel: hexBg("#471b1b"),
		},
		{
			ID:          "monokai",
			Name:        "Monokai Pro",
			Description: "Тёплый угольный с яркими золотистыми акцентами",
			BgPrimary:   hexBg("#19181a"),
			BgSurface:   hexBg("#221f22"),
			BgPanelAlt:  hexBg("#1e1d1f"),
			BgCard:      hexBg("#2d2a2e"),
			BgHighlight: hexBg("#403e41"),
			BgSelected:  hexBg("#555057"),
			BgModal:     hexBg("#221f22"),
			BgModalHdr:  hexBg("#2d2a2e"),
			BgInput:     hexBg("#19181a"),
			FgMain:      hexFg("#fcfcfa"),
			FgMuted:     hexFg("#c1c0c0"),
			FgDim:       hexFg("#727072"),
			FgWhite:     hexFg("#ffffff"),
			FgBlue:      hexFg("#78dce8"),
			FgCyan:      hexFg("#78dce8"),
			FgGreen:     hexFg("#a9dc76"),
			FgYellow:    hexFg("#ffd866"),
			FgOrange:    hexFg("#fc9867"),
			FgMagenta:   hexFg("#ab9df2"),
			FgRed:       hexFg("#ff6188"),
			BorderDim:    hexFg("#3a373b"),
			BorderBright: hexFg("#5b575d"),
			BorderFocus:  hexFg("#ffd866"),
			StatusBgActive: hexBg("#20363a"),
			StatusBgDone:   hexBg("#2c3f1e"),
			StatusBgProg:   hexBg("#4a3f1b"),
			StatusBgPlan:   hexBg("#1c393c"),
			StatusBgMissed: hexBg("#422619"),
			StatusBgCancel: hexBg("#451a24"),
		},
		{
			ID:          "solarized",
			Name:        "Solarized Dark",
			Description: "Глубокий сине-бирюзовый с мягкой контрастностью",
			BgPrimary:   hexBg("#00212b"),
			BgSurface:   hexBg("#002b36"),
			BgPanelAlt:  hexBg("#001e26"),
			BgCard:      hexBg("#073642"),
			BgHighlight: hexBg("#0b4352"),
			BgSelected:  hexBg("#145365"),
			BgModal:     hexBg("#002b36"),
			BgModalHdr:  hexBg("#073642"),
			BgInput:     hexBg("#001e26"),
			FgMain:      hexFg("#eee8d5"),
			FgMuted:     hexFg("#93a1a1"),
			FgDim:       hexFg("#586e75"),
			FgWhite:     hexFg("#ffffff"),
			FgBlue:      hexFg("#268bd2"),
			FgCyan:      hexFg("#2aa198"),
			FgGreen:     hexFg("#859900"),
			FgYellow:    hexFg("#b58900"),
			FgOrange:    hexFg("#cb4b16"),
			FgMagenta:   hexFg("#d33682"),
			FgRed:       hexFg("#dc322f"),
			BorderDim:    hexFg("#0a404e"),
			BorderBright: hexFg("#1d5f73"),
			BorderFocus:  hexFg("#268bd2"),
			StatusBgActive: hexBg("#083d59"),
			StatusBgDone:   hexBg("#263f06"),
			StatusBgProg:   hexBg("#473600"),
			StatusBgPlan:   hexBg("#0c423f"),
			StatusBgMissed: hexBg("#471d07"),
			StatusBgCancel: hexBg("#4c1211"),
		},
		{
			ID:          "cyberpunk",
			Name:        "Cyberpunk Neon",
			Description: "Ядовитый неон индиго, бирюзы и фуксии",
			BgPrimary:   hexBg("#08080f"),
			BgSurface:   hexBg("#12131e"),
			BgPanelAlt:  hexBg("#0d0e17"),
			BgCard:      hexBg("#1a1a2b"),
			BgHighlight: hexBg("#282845"),
			BgSelected:  hexBg("#392854"),
			BgModal:     hexBg("#12131e"),
			BgModalHdr:  hexBg("#1a1a2b"),
			BgInput:     hexBg("#07070d"),
			FgMain:      hexFg("#ffffff"),
			FgMuted:     hexFg("#b8b8d0"),
			FgDim:       hexFg("#717199"),
			FgWhite:     hexFg("#ffffff"),
			FgBlue:      hexFg("#00f0ff"),
			FgCyan:      hexFg("#00f0ff"),
			FgGreen:     hexFg("#00ff66"),
			FgYellow:    hexFg("#fcee0a"),
			FgOrange:    hexFg("#ff7700"),
			FgMagenta:   hexFg("#ff0055"),
			FgRed:       hexFg("#ff003c"),
			BorderDim:    hexFg("#252540"),
			BorderBright: hexFg("#4a3d6e"),
			BorderFocus:  hexFg("#00f0ff"),
			StatusBgActive: hexBg("#003844"),
			StatusBgDone:   hexBg("#00401d"),
			StatusBgProg:   hexBg("#443f00"),
			StatusBgPlan:   hexBg("#003a3d"),
			StatusBgMissed: hexBg("#4d2200"),
			StatusBgCancel: hexBg("#4d0014"),
		},
		{
			ID:          "matrix",
			Name:        "Matrix Emerald",
			Description: "Чёрный консольный терминал с изумрудным свечением",
			BgPrimary:   hexBg("#040d06"),
			BgSurface:   hexBg("#09170c"),
			BgPanelAlt:  hexBg("#061208"),
			BgCard:      hexBg("#0e2413"),
			BgHighlight: hexBg("#14361f"),
			BgSelected:  hexBg("#1c4a2a"),
			BgModal:     hexBg("#09170c"),
			BgModalHdr:  hexBg("#0e2413"),
			BgInput:     hexBg("#030805"),
			FgMain:      hexFg("#d1fae5"),
			FgMuted:     hexFg("#6ee7b7"),
			FgDim:       hexFg("#226e43"),
			FgWhite:     hexFg("#ffffff"),
			FgBlue:      hexFg("#34d399"),
			FgCyan:      hexFg("#6ee7b7"),
			FgGreen:     hexFg("#10b981"),
			FgYellow:    hexFg("#a7f3d0"),
			FgOrange:    hexFg("#34d399"),
			FgMagenta:   hexFg("#6ee7b7"),
			FgRed:       hexFg("#f87171"),
			BorderDim:    hexFg("#183e24"),
			BorderBright: hexFg("#255d37"),
			BorderFocus:  hexFg("#34d399"),
			StatusBgActive: hexBg("#0b3820"),
			StatusBgDone:   hexBg("#083b1a"),
			StatusBgProg:   hexBg("#2d3d14"),
			StatusBgPlan:   hexBg("#0b3b2a"),
			StatusBgMissed: hexBg("#3b2609"),
			StatusBgCancel: hexBg("#3d1010"),
		},
		{
			ID:          "paper",
			Name:        "Paper Light",
			Description: "Светлый элегантный стиль для светлых терминалов",
			BgPrimary:   hexBg("#f4f6f8"),
			BgSurface:   hexBg("#ffffff"),
			BgPanelAlt:  hexBg("#eaedf1"),
			BgCard:      hexBg("#e4e8ee"),
			BgHighlight: hexBg("#d8dee8"),
			BgSelected:  hexBg("#c7d5e8"),
			BgModal:     hexBg("#ffffff"),
			BgModalHdr:  hexBg("#eaedf1"),
			BgInput:     hexBg("#ffffff"),
			FgMain:      hexFg("#24292f"),
			FgMuted:     hexFg("#57606a"),
			FgDim:       hexFg("#8c959f"),
			FgWhite:     hexFg("#0f141c"),
			FgBlue:      hexFg("#0969da"),
			FgCyan:      hexFg("#1b78af"),
			FgGreen:     hexFg("#1a7f37"),
			FgYellow:    hexFg("#9a6700"),
			FgOrange:    hexFg("#bc4c00"),
			FgMagenta:   hexFg("#8220c7"),
			FgRed:       hexFg("#cf222e"),
			BorderDim:    hexFg("#cbd5e1"),
			BorderBright: hexFg("#94a3b8"),
			BorderFocus:  hexFg("#2563eb"),
			StatusBgActive: hexBg("#ddf4ff"),
			StatusBgDone:   hexBg("#dafbe1"),
			StatusBgProg:   hexBg("#fff8c5"),
			StatusBgPlan:   hexBg("#ddf4ff"),
			StatusBgMissed: hexBg("#ffebe9"),
			StatusBgCancel: hexBg("#ffebe9"),
		},
	}
)

// AvailableThemes returns all defined themes.
func AvailableThemes() []ThemeDef {
	return availableThemes
}

// CurrentTheme returns the active theme ID.
func CurrentTheme() string {
	return activeThemeID
}

// ApplyTheme activates the specified theme palette.
func ApplyTheme(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	for _, t := range availableThemes {
		if t.ID == id {
			activeThemeID = t.ID
			BgPrimary = t.BgPrimary
			BgSurface = t.BgSurface
			BgPanelAlt = t.BgPanelAlt
			BgCard = t.BgCard
			BgHighlight = t.BgHighlight
			BgSelected = t.BgSelected
			BgModal = t.BgModal
			BgModalHdr = t.BgModalHdr
			BgInput = t.BgInput

			FgMain = t.FgMain
			FgMuted = t.FgMuted
			FgDim = t.FgDim
			FgWhite = t.FgWhite
			FgBlue = t.FgBlue
			FgCyan = t.FgCyan
			FgGreen = t.FgGreen
			FgYellow = t.FgYellow
			FgOrange = t.FgOrange
			FgMagenta = t.FgMagenta
			FgRed = t.FgRed

			BorderDim = t.BorderDim
			BorderBright = t.BorderBright
			BorderFocus = t.BorderFocus

			StatusBgActive = t.StatusBgActive
			StatusBgDone = t.StatusBgDone
			StatusBgProg = t.StatusBgProg
			StatusBgPlan = t.StatusBgPlan
			StatusBgMissed = t.StatusBgMissed
			StatusBgCancel = t.StatusBgCancel
			return t.ID
		}
	}
	// Fallback to tokyo
	return ApplyTheme("tokyo")
}

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
		return "● Активно", FgBlue, StatusBgActive
	case strings.EqualFold(s, "Завершено") || strings.EqualFold(s, "Completed") || strings.EqualFold(s, "Done"):
		return "✓ Завершено", FgGreen, StatusBgDone
	case strings.EqualFold(s, "В процессе") || strings.EqualFold(s, "In Progress"):
		return "◐ В процессе", FgYellow, StatusBgProg
	case strings.EqualFold(s, "Запланировано") || strings.EqualFold(s, "Planned"):
		return "◷ Запланировано", FgCyan, StatusBgPlan
	case strings.EqualFold(s, "Пропущено") || strings.EqualFold(s, "Missed"):
		return "⊘ Пропущено", FgOrange, StatusBgMissed
	case strings.EqualFold(s, "Отменено") || strings.EqualFold(s, "Cancelled"):
		return "✗ Отменено", FgRed, StatusBgCancel
	case s == "" || strings.EqualFold(s, "~пусто~") || strings.EqualFold(s, "<пусто>"):
		return "~пусто~", FgDim, BgCard
	default:
		return "• " + s, FgMain, BgCard
	}
}

// RuneVisualWidth returns the number of terminal columns required to display rune r.
func RuneVisualWidth(r rune) int {
	if r == 0 || r == '\uFE0F' || (r >= 0x200B && r <= 0x200D) {
		return 0
	}
	// Combining characters
	if (r >= 0x0300 && r <= 0x036F) || (r >= 0x1AB0 && r <= 0x1AFF) ||
		(r >= 0x1DC0 && r <= 0x1DFF) || (r >= 0x20D0 && r <= 0x20FF) ||
		(r >= 0xFE20 && r <= 0xFE2F) {
		return 0
	}

	// Wide emojis in miscellaneous blocks (watch, hourglass, etc.)
	if r == 0x231A || r == 0x231B || (r >= 0x23E9 && r <= 0x23EC) || r == 0x23F0 || r == 0x23F3 {
		return 2
	}
	if (r >= 0x2614 && r <= 0x2615) || (r >= 0x2648 && r <= 0x2653) || r == 0x267F ||
		r == 0x2693 || r == 0x26A1 || r == 0x26AA || r == 0x26AB || r == 0x26BD ||
		r == 0x26BE || r == 0x26C4 || r == 0x26C5 || r == 0x26CE || r == 0x26D4 ||
		r == 0x26EA || r == 0x26F2 || r == 0x26F3 || r == 0x26F5 || r == 0x26FA || r == 0x26FD {
		return 2
	}

	// Wide characters: emojis, symbols, and CJK ideographs (East Asian Width Wide/Fullwidth)
	if (r >= 0x1100 && r <= 0x115F) ||
		(r >= 0x2E80 && r <= 0xA4CF && (r < 0x3000 || r > 0x303E || r == 0x3000)) ||
		(r >= 0xAC00 && r <= 0xD7A3) ||
		(r >= 0xF900 && r <= 0xFAFF) ||
		(r >= 0xFE10 && r <= 0xFE19) ||
		(r >= 0xFE30 && r <= 0xFE6F) ||
		(r >= 0xFF00 && r <= 0xFF60) ||
		(r >= 0xFFE0 && r <= 0xFFE6) ||
		(r >= 0x1F300 && r <= 0x1FAFF) ||
		(r >= 0x20000 && r <= 0x3FFFD) {
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
