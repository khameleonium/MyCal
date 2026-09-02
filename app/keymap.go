package app

import "strings"

// cyr2lat folds a Cyrillic (ЙЦУКЕН) character to the Latin (QWERTY) key in the
// same physical keyboard position, so a hotkey works regardless of layout.
// Submenus historically relied on this positional mapping; the main menu also
// accepts the mnemonic first letter of each Russian label (see Run).
var cyr2lat = map[rune]rune{
	'й': 'q', 'ц': 'w', 'у': 'e', 'к': 'r', 'е': 't', 'н': 'y', 'г': 'u',
	'ш': 'i', 'щ': 'o', 'з': 'p', 'х': '[', 'ъ': ']',
	'ф': 'a', 'ы': 's', 'в': 'd', 'а': 'f', 'п': 'g', 'р': 'h', 'о': 'j',
	'л': 'k', 'д': 'l', 'ж': ';', 'э': '\'',
	'я': 'z', 'ч': 'x', 'с': 'c', 'м': 'v', 'и': 'b', 'т': 'n', 'ь': 'm',
	'б': ',', 'ю': '.',
}

// fold lower-cases s and maps any Cyrillic letters to their QWERTY positions.
func fold(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		if l, ok := cyr2lat[r]; ok {
			b.WriteRune(l)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// match reports whether input equals any of keys after layout folding.
func match(input string, keys ...string) bool {
	f := fold(input)
	for _, k := range keys {
		if f == fold(k) {
			return true
		}
	}
	return false
}
