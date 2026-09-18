package app

import (
	"testing"
	"time"
)

// Every key a main-menu entry declares must reach that entry. A Cyrillic
// mnemonic folds to the QWERTY key in the same physical position ("в" -> "d"),
// so without exact-match-first resolution "в" (Выход) opened Удалить and "м"
// (Справка) opened Просмотр.
func TestMainMenuKeysReachTheirOwnEntry(t *testing.T) {
	keys := mainMenuKeys()
	for i, item := range mainMenuItems {
		for _, k := range item.keys {
			if got := resolve(k, keys); got != i {
				t.Errorf("key %q belongs to %q (index %d) but resolved to index %d",
					k, item.label, i, got)
			}
		}
	}
}

// Positional folding must keep working for keys that are not declared
// mnemonics: "ф" sits on the "a" key, which is Добавить.
func TestResolveFallsBackToLayoutFolding(t *testing.T) {
	keys := mainMenuKeys()
	if got := resolve("ф", keys); got != 0 {
		t.Errorf("positional fold of %q: got index %d, want 0", "ф", got)
	}
	if got := resolve("zzz", keys); got != -1 {
		t.Errorf("unknown key: got index %d, want -1", got)
	}
}

func TestRepeatDatesMonthlyClampsInsteadOfDrifting(t *testing.T) {
	start := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)
	until := time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC)

	got, truncated := repeatDates(start, until, "m")
	if truncated {
		t.Fatal("unexpected truncation")
	}
	want := []string{"2026-02-28", "2026-03-31", "2026-04-30", "2026-05-31"}
	if len(got) != len(want) {
		t.Fatalf("got %d occurrences %v, want %d %v", len(got), fmtDates(got), len(want), want)
	}
	for i, w := range want {
		if g := got[i].Format("2006-01-02"); g != w {
			t.Errorf("occurrence %d: got %s, want %s", i, g, w)
		}
	}
}

func TestRepeatDatesDailyAndCap(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	got, truncated := repeatDates(start, start.AddDate(0, 0, 3), "d")
	if truncated || len(got) != 3 {
		t.Fatalf("daily: got %d dates (truncated=%v), want 3", len(got), truncated)
	}

	got, truncated = repeatDates(start, start.AddDate(100, 0, 0), "d")
	if !truncated {
		t.Error("a 100-year daily series should report truncation")
	}
	if len(got) != maxOccurrences {
		t.Errorf("cap: got %d occurrences, want exactly %d", len(got), maxOccurrences)
	}
}

func fmtDates(ts []time.Time) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.Format("2006-01-02")
	}
	return out
}
