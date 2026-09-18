package app

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"mycalendar/calendar"
	"mycalendar/config"
	"mycalendar/models"
)

// feed replaces the app's stdin with a fresh script.
func (a *App) feed(script string) {
	a.in = bufio.NewReader(strings.NewReader(script))
	a.stdinClosed = false
}

// A status typed by hand and accepted into the list must survive the session:
// it used to be appended to the in-memory config and never written to disk.
func TestCustomStatusIsPersisted(t *testing.T) {
	a, dir := newTestApp(t, "")
	a.cfgPath = filepath.Join(dir, config.ConfigFile)

	// status picker: "Ручной ввод" is the entry after the seeded statuses.
	manual := len(a.svc.Statuses(a.cfg.CustomStatuses...)) + 1
	a.feed(lines(strconv.Itoa(manual), "Перенесено", "да"))

	st, ok := a.askStatus("")
	if !ok || st != "Перенесено" {
		t.Fatalf("askStatus returned (%q, %v)", st, ok)
	}

	saved, err := config.Load(context.Background(), a.cfgPath)
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if !containsFold(saved.CustomStatuses, "Перенесено") {
		t.Errorf("custom status not written to %s: %+v", a.cfgPath, saved.CustomStatuses)
	}
}

// "Запомнить выбор и впредь исправлять автоматически?" must actually be
// remembered across runs.
func TestRememberedDateCheckModeIsPersisted(t *testing.T) {
	a, dir := newTestApp(t, "")
	a.cfgPath = filepath.Join(dir, config.ConfigFile)
	a.cfg.DateCheckMode = models.DateCheckAsk

	// 32.01.2026 rolls over to 01.02.2026: accept it, then say yes to remember.
	a.feed(lines("32.01.2026", "1", "да"))
	if _, ok := a.askDate("Дата:", ""); !ok {
		t.Fatal("askDate aborted")
	}
	if a.cfg.DateCheckMode != models.DateCheckFix {
		t.Fatalf("in-memory mode is %q, want %q", a.cfg.DateCheckMode, models.DateCheckFix)
	}

	saved, err := config.Load(context.Background(), a.cfgPath)
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if saved.DateCheckMode != models.DateCheckFix {
		t.Errorf("remembered choice not written to disk: got %q", saved.DateCheckMode)
	}
}

// A comment could never be removed: an empty reply meant "unchanged" and "0"
// cancelled, so there was no way to express "clear it".
func TestNotesCanBeCleared(t *testing.T) {
	a, _ := newTestApp(t, "")

	a.feed(lines("-"))
	v, ok, changed := a.askNotesChange("старый комментарий")
	if !ok || !changed || v != "" {
		t.Errorf(`clearing with "-": got (%q, ok=%v, changed=%v), want ("", true, true)`, v, ok, changed)
	}

	a.feed(lines(""))
	if _, ok, changed := a.askNotesChange("старый"); !ok || changed {
		t.Errorf("empty reply should keep the note: ok=%v changed=%v", ok, changed)
	}

	a.feed(lines("0"))
	if _, ok, _ := a.askNotesChange("старый"); ok {
		t.Error(`"0" should cancel`)
	}

	// Clearing an already-empty note is not a change.
	a.feed(lines("-"))
	if _, ok, changed := a.askNotesChange(""); !ok || changed {
		t.Errorf("clearing an empty note: ok=%v changed=%v, want ok=true changed=false", ok, changed)
	}
}

// Editing through the list must act on current data, not on the snapshot taken
// when the list was printed.
func TestDetailLoopUsesFreshDataAfterDelete(t *testing.T) {
	dir := t.TempDir()
	svc, err := calendar.NewService(context.Background(), dir, "mycal", models.SplitNone)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	session := models.Session{ID: "20260101120000", Time: "12:00", Name: "Встреча", Duration: 30}
	if err := svc.Add(session); err != nil {
		t.Fatalf("Add: %v", err)
	}
	a := &App{svc: svc, cfg: config.DefaultConfig(), cfgPath: filepath.Join(dir, config.ConfigFile), ctx: context.Background()}

	snapshot := svc.All()
	svc.Delete(session.ID)

	// Asking for row 1 of the stale snapshot must not print a deleted record.
	a.feed(lines("1", ""))
	out := captureStdout(t, func() { a.detailLoop(snapshot) })
	if strings.Contains(out, "Встреча") {
		t.Errorf("detail loop showed a deleted session from the stale snapshot:\n%s", out)
	}
	if !strings.Contains(out, "не найдена") {
		t.Errorf("expected a not-found message, got:\n%s", out)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		var sb strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := r.Read(buf)
			sb.Write(buf[:n])
			if err != nil {
				break
			}
		}
		done <- sb.String()
	}()
	fn()
	w.Close()
	os.Stdout = orig
	return <-done
}
