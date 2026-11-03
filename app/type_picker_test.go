package app

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"mycalendar/calendar"
	"mycalendar/config"
	"mycalendar/models"
)

func TestParseTypeIndices(t *testing.T) {
	tests := []struct {
		input     string
		maxLen    int
		wantIdx   []int
		isIndices bool
		wantErr   bool
	}{
		{"1", 5, []int{1}, true, false},
		{"1, 2, 3", 5, []int{1, 2, 3}, true, false},
		{"1 3", 5, []int{1, 3}, true, false},
		{"1-3", 5, []int{1, 2, 3}, true, false},
		{"1 – 3", 5, []int{1, 2, 3}, true, false},
		{"1 — 3", 5, []int{1, 2, 3}, true, false},
		{"1, 3-5", 5, []int{1, 3, 4, 5}, true, false},
		{"2-2", 5, []int{2}, true, false},
		// Letters -> text filter
		{"стрельб", 5, nil, false, false},
		{"лук", 5, nil, false, false},
		{"Type 1", 5, nil, false, false},
		// Numbers out of bounds
		{"6", 5, nil, true, true},
		{"0", 5, nil, true, true},
		{"1-6", 5, nil, true, true},
		{"-1", 5, nil, true, true},
	}

	for _, tc := range tests {
		got, isIndices, err := parseTypeIndices(tc.input, tc.maxLen)
		if isIndices != tc.isIndices {
			t.Errorf("parseTypeIndices(%q, %d): got isIndices=%v, want %v",
				tc.input, tc.maxLen, isIndices, tc.isIndices)
		}
		if (err != nil) != tc.wantErr {
			t.Errorf("parseTypeIndices(%q, %d): got err=%v, wantErr=%v",
				tc.input, tc.maxLen, err, tc.wantErr)
		}
		if isIndices && !tc.wantErr && !reflect.DeepEqual(got, tc.wantIdx) {
			t.Errorf("parseTypeIndices(%q, %d): got indices=%v, want %v",
				tc.input, tc.maxLen, got, tc.wantIdx)
		}
	}
}

func TestFilterSessionsByTypes(t *testing.T) {
	sessions := []models.Session{
		{ID: "20261101100000", Name: "Тренировка 1", Type: "Стрельба из лука"},
		{ID: "20261102110000", Name: "Тренировка 2", Type: "Метание ножей"},
		{ID: "20261103120000", Name: "Тренировка 3", Type: "Плавание"},
		{ID: "20261104130000", Name: "Дело", Type: ""},
	}

	// 1. Filter by single type (case-insensitive)
	res1 := filterSessionsByTypes(sessions, []string{"стрельба из лука"})
	if len(res1) != 1 || res1[0].ID != "20261101100000" {
		t.Fatalf("filter single type: got %+v", res1)
	}

	// 2. Filter by multiple types
	res2 := filterSessionsByTypes(sessions, []string{"Стрельба из лука", "Плавание"})
	if len(res2) != 2 || res2[0].Type != "Стрельба из лука" || res2[1].Type != "Плавание" {
		t.Fatalf("filter multi types: got %+v", res2)
	}

	// 3. Filter by "(без типа)"
	res3 := filterSessionsByTypes(sessions, []string{"(без типа)"})
	if len(res3) != 1 || res3[0].ID != "20261104130000" {
		t.Fatalf("filter empty type: got %+v", res3)
	}

	// 4. Non-matching type
	res4 := filterSessionsByTypes(sessions, []string{"Шахматы"})
	if len(res4) != 0 {
		t.Fatalf("filter non-matching: got %+v", res4)
	}
}

func TestExtractTypeArg(t *testing.T) {
	typeVal, rem := extractTypeArg([]string{"--type", "Стрельба", "15-20"})
	if typeVal != "Стрельба" || len(rem) != 1 || rem[0] != "15-20" {
		t.Errorf("extractTypeArg: got %q, %v", typeVal, rem)
	}

	typeVal, rem = extractTypeArg([]string{"-t=Плавание", "2026"})
	if typeVal != "Плавание" || len(rem) != 1 || rem[0] != "2026" {
		t.Errorf("extractTypeArg: got %q, %v", typeVal, rem)
	}

	typeVal, rem = extractTypeArg([]string{"--тип", "Ножи"})
	if typeVal != "Ножи" || len(rem) != 0 {
		t.Errorf("extractTypeArg: got %q, %v", typeVal, rem)
	}

	typeVal, rem = extractTypeArg([]string{"15-20"})
	if typeVal != "" || len(rem) != 1 || rem[0] != "15-20" {
		t.Errorf("extractTypeArg: got %q, %v", typeVal, rem)
	}
}

func TestTrimPrefixWord(t *testing.T) {
	if after, ok := trimPrefixWord("t стрельб", "t", "тип"); !ok || after != "стрельб" {
		t.Errorf("trimPrefixWord 't стрельб': got %q, %v", after, ok)
	}
	if after, ok := trimPrefixWord("тип: лук", "t", "тип"); !ok || after != "лук" {
		t.Errorf("trimPrefixWord 'тип: лук': got %q, %v", after, ok)
	}
	if after, ok := trimPrefixWord("today", "t", "тип"); ok {
		t.Errorf("trimPrefixWord 'today': expected false, got %q", after)
	}
}

func TestFormatTypesLabel(t *testing.T) {
	if got := formatTypesLabel("Все записи", []string{"Стрельба"}); got != "Все записи [Тип: Стрельба]" {
		t.Errorf("formatTypesLabel 1: got %q", got)
	}
	if got := formatTypesLabel("За месяц", []string{"А", "Б"}); got != "За месяц [Типы: А, Б]" {
		t.Errorf("formatTypesLabel 2: got %q", got)
	}
	if got := formatTypesLabel("", []string{"А", "Б", "В", "Г"}); got != "Выбрано типов: 4" {
		t.Errorf("formatTypesLabel 4: got %q", got)
	}
}

func TestPickTypesMulti_Interactive(t *testing.T) {
	allTypes := []string{"Бег", "Метание ножей", "Плавание", "Стрельба из лука"}

	// 1. Direct index selection: "2", then Enter (confirm)
	script1 := lines("2", "")
	a1, _ := newTestApp(t, script1)
	picked, ok := a1.pickTypesMulti(allTypes, "Тест")
	if !ok || len(picked) != 1 || picked[0] != "Метание ножей" {
		t.Fatalf("pickTypesMulti direct: got %v, ok=%v", picked, ok)
	}

	// 2. Search filter: "стрельб", then "1", then Enter
	script2 := lines("стрельб", "1", "")
	a2, _ := newTestApp(t, script2)
	picked2, ok2 := a2.pickTypesMulti(allTypes, "Тест")
	if !ok2 || len(picked2) != 1 || picked2[0] != "Стрельба из лука" {
		t.Fatalf("pickTypesMulti search: got %v, ok=%v", picked2, ok2)
	}

	// 3. Bulk select '+': all visible become selected
	script3 := lines("+", "")
	a3, _ := newTestApp(t, script3)
	picked3, ok3 := a3.pickTypesMulti(allTypes, "Тест")
	if !ok3 || len(picked3) != len(allTypes) {
		t.Fatalf("pickTypesMulti '+': got %v, ok=%v", picked3, ok3)
	}

	// 4. Cancel '0'
	script4 := lines("0")
	a4, _ := newTestApp(t, script4)
	picked4, ok4 := a4.pickTypesMulti(allTypes, "Тест")
	if ok4 || len(picked4) != 0 {
		t.Fatalf("pickTypesMulti cancel: expected false, got %v, ok=%v", picked4, ok4)
	}
}

func TestViewByTypes_Interactive(t *testing.T) {
	dir := t.TempDir()
	svc, err := calendar.NewService(context.Background(), dir, "mycal", models.SplitNone)
	if err != nil {
		t.Fatal(err)
	}
	_ = svc.Add(models.Session{ID: "20261101100000", Name: "Сессия 1", Type: "Стрельба из лука", Duration: 30})
	_ = svc.Add(models.Session{ID: "20261102110000", Name: "Сессия 2", Type: "Плавание", Duration: 45})
	_ = svc.Save(context.Background())

	// View menu:
	// "5" (по типам)
	// pickTypes: "стрельб", "1", "" (confirm)
	// period: "4" (все записи)
	// detailLoop: "" (Enter to back)
	// main menu: "8" (exit)
	script := lines("2", "5", "стрельб", "1", "", "4", "", "8")
	cfg := config.DefaultConfig()
	a := &App{
		svc:     svc,
		cfg:     cfg,
		cfgPath: filepath.Join(dir, config.ConfigFile),
		ctx:     context.Background(),
		in:      bufio.NewReader(strings.NewReader(script)),
	}
	a.mainMenu()

	if a.svc.Count() != 2 {
		t.Fatalf("expected 2 sessions to remain untouched, got %d", a.svc.Count())
	}
}

func TestFilterByTypesInsideList_Interactive(t *testing.T) {
	dir := t.TempDir()
	svc, err := calendar.NewService(context.Background(), dir, "mycal", models.SplitNone)
	if err != nil {
		t.Fatal(err)
	}
	_ = svc.Add(models.Session{ID: "20261101100000", Name: "Сессия 1", Type: "Стрельба из лука", Duration: 30})
	_ = svc.Add(models.Session{ID: "20261102110000", Name: "Сессия 2", Type: "Плавание", Duration: 45})
	_ = svc.Save(context.Background())

	// Flow:
	// 1. Main menu: "2" (Посмотреть записи)
	// 2. View menu: "4" (Все записи)
	// 3. Inside record list: press "t" (Фильтр по типам)
	// 4. Type picker opens: "1" (Плавание/Стрельба), "" (confirm)
	// 5. Filtered view: "" (Enter to back)
	// 6. Back in All records: "" (Enter to back)
	// 7. Back in View menu: "0" (Назад)
	// 8. Main menu: "8" (Выход)
	script := lines("2", "4", "t", "1", "", "", "", "0", "8")
	cfg := config.DefaultConfig()
	a := &App{
		svc:     svc,
		cfg:     cfg,
		cfgPath: filepath.Join(dir, config.ConfigFile),
		ctx:     context.Background(),
		in:      bufio.NewReader(strings.NewReader(script)),
	}
	a.mainMenu()

	if a.svc.Count() != 2 {
		t.Fatalf("expected 2 sessions to remain untouched, got %d", a.svc.Count())
	}
}

func TestExportByTypes_Interactive(t *testing.T) {
	dir := t.TempDir()
	svc, err := calendar.NewService(context.Background(), dir, "mycal", models.SplitNone)
	if err != nil {
		t.Fatal(err)
	}
	_ = svc.Add(models.Session{ID: "20261101100000", Name: "Сессия 1", Type: "Стрельба из лука", Duration: 30})
	_ = svc.Add(models.Session{ID: "20261102110000", Name: "Сессия 2", Type: "Плавание", Duration: 45})
	_ = svc.Save(context.Background())

	// Export menu:
	// "5" (экспорт) -> "5" (по типам)
	// pickTypes: "стрельб", "1", "" (confirm)
	// period: "4" (все)
	// main menu: "8" (exit)
	script := lines("5", "5", "стрельб", "1", "", "4", "8")
	cfg := config.DefaultConfig()
	a := &App{
		svc:     svc,
		cfg:     cfg,
		cfgPath: filepath.Join(dir, config.ConfigFile),
		ctx:     context.Background(),
		in:      bufio.NewReader(strings.NewReader(script)),
	}
	a.mainMenu()

	matches, _ := filepath.Glob(filepath.Join(dir, "export_*.csv"))
	if len(matches) == 0 {
		t.Fatal("expected exported CSV file")
	}
	content, _ := os.ReadFile(matches[0])
	if len(content) == 0 {
		t.Fatal("exported CSV file is empty")
	}
}
