package app

import "testing"

func TestMatchLayoutFolding(t *testing.T) {
	cases := []struct {
		input string
		keys  []string
		want  bool
	}{
		{"1", []string{"1", "w"}, true},
		{"w", []string{"1", "w"}, true},
		{"ц", []string{"1", "w"}, true},   // ц sits on the w key
		{"W", []string{"1", "w"}, true},   // case-insensitive
		{" ц ", []string{"1", "w"}, true}, // trimmed
		{"й", []string{"0", "q"}, true},   // й on q
		{"ф", []string{"a"}, true},        // ф on a
		{"z", []string{"a", "w"}, false},
		{"", []string{"1"}, false},
	}
	for _, c := range cases {
		if got := match(c.input, c.keys...); got != c.want {
			t.Errorf("match(%q, %v) = %v, want %v", c.input, c.keys, got, c.want)
		}
	}
}

func TestIsYes(t *testing.T) {
	for _, s := range []string{"да", "Да", " д ", "yes", "Y"} {
		if !isYes(s) {
			t.Errorf("isYes(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"", "n", "нет", "maybe"} {
		if isYes(s) {
			t.Errorf("isYes(%q) = true, want false", s)
		}
	}
}
