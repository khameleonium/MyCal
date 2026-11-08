package locale

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestEmbeddedLocalesLoaded(t *testing.T) {
	mgr := NewManager()

	langs := mgr.AvailableLanguages()
	if len(langs) < 2 {
		t.Fatalf("expected at least 2 embedded languages (ru, en), got %d", len(langs))
	}

	ruFound, enFound := false, false
	for _, l := range langs {
		if l.Code == "ru" {
			ruFound = true
			if l.Name == "" {
				t.Errorf("expected non-empty name for ru")
			}
		}
		if l.Code == "en" {
			enFound = true
			if l.Name == "" {
				t.Errorf("expected non-empty name for en")
			}
		}
	}

	if !ruFound || !enFound {
		t.Fatalf("embedded ru or en not found: ru=%v, en=%v", ruFound, enFound)
	}

	// Verify key translations in Russian
	mgr.SetLanguage("ru")
	if val := mgr.T("menu.add"); val != "Добавить запись" {
		t.Errorf("expected 'Добавить запись', got '%s'", val)
	}
	if val := mgr.T("common.save"); val != "Сохранить" {
		t.Errorf("expected 'Сохранить', got '%s'", val)
	}

	// Verify key translations in English
	mgr.SetLanguage("en")
	if val := mgr.T("menu.add"); val != "Add entry" {
		t.Errorf("expected 'Add entry', got '%s'", val)
	}
	if val := mgr.T("common.save"); val != "Save" {
		t.Errorf("expected 'Save', got '%s'", val)
	}
}

func TestTranslateFallback(t *testing.T) {
	mgr := NewManager()
	mgr.SetLanguage("en")

	// Key that doesn't exist anywhere
	if val := mgr.T("non.existent.key"); val != "non.existent.key" {
		t.Errorf("expected 'non.existent.key', got '%s'", val)
	}

	// Key with TDef
	if val := mgr.TDef("missing.key", "Default Fallback"); val != "Default Fallback" {
		t.Errorf("expected 'Default Fallback', got '%s'", val)
	}
}

func TestExternalOverrideAndNewLanguage(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. Create external override for Russian
	ruOverride := `{
		"_meta": {
			"code": "ru",
			"name": "Русский (Кастом)",
			"author": "Tester"
		},
		"menu": {
			"add": "Внести событие"
		}
	}`
	ruFile := filepath.Join(tmpDir, "mycal_locale--ru.json")
	if err := os.WriteFile(ruFile, []byte(ruOverride), 0644); err != nil {
		t.Fatal(err)
	}

	// 2. Create brand new language: French
	frContent := `{
		"_meta": {
			"code": "fr",
			"name": "Français",
			"author": "French Translator"
		},
		"menu": {
			"add": "Ajouter une entrée"
		},
		"common": {
			"save": "Enregistrer"
		}
	}`
	frFile := filepath.Join(tmpDir, "mycal_locale--fr.json")
	if err := os.WriteFile(frFile, []byte(frContent), 0644); err != nil {
		t.Fatal(err)
	}

	mgr := NewManager()
	mgr.LoadExternal(tmpDir)

	// Verify Russian override worked
	mgr.SetLanguage("ru")
	if val := mgr.T("menu.add"); val != "Внести событие" {
		t.Errorf("expected overridden 'Внести событие', got '%s'", val)
	}
	// Embedded keys should remain intact in Russian
	if val := mgr.T("menu.view"); val != "Посмотреть записи" {
		t.Errorf("expected preserved 'Посмотреть записи', got '%s'", val)
	}

	// Verify French was registered
	langs := mgr.AvailableLanguages()
	frFound := false
	for _, l := range langs {
		if l.Code == "fr" {
			frFound = true
			if l.Name != "Français" {
				t.Errorf("expected 'Français', got '%s'", l.Name)
			}
		}
	}
	if !frFound {
		t.Fatalf("expected French (fr) in available languages")
	}

	// Verify French translation and fallbacks
	mgr.SetLanguage("fr")
	if val := mgr.T("menu.add"); val != "Ajouter une entrée" {
		t.Errorf("expected 'Ajouter une entrée', got '%s'", val)
	}
	if val := mgr.T("common.save"); val != "Enregistrer" {
		t.Errorf("expected 'Enregistrer', got '%s'", val)
	}
	// Fallback to ru for untranslated keys
	if val := mgr.T("menu.delete"); val != "Удалить запись" {
		t.Errorf("expected fallback 'Удалить запись', got '%s'", val)
	}
}

func TestExportTemplate(t *testing.T) {
	mgr := NewManager()
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "mycal_locale--de.json")

	err := mgr.ExportTemplate("en", outPath, "de", "Deutsch")
	if err != nil {
		t.Fatalf("ExportTemplate failed: %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("failed to read exported file: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("exported template is not valid JSON: %v", err)
	}

	meta, ok := parsed["_meta"].(map[string]any)
	if !ok {
		t.Fatalf("exported template missing _meta block")
	}
	if meta["code"] != "de" || meta["name"] != "Deutsch" {
		t.Errorf("unexpected meta in template: code=%v, name=%v", meta["code"], meta["name"])
	}

	menu, ok := parsed["menu"].(map[string]any)
	if !ok {
		t.Fatalf("exported template missing menu block")
	}
	if menu["add"] != "Add entry" {
		t.Errorf("expected base en string 'Add entry', got %v", menu["add"])
	}
}

func TestGetAllStrings(t *testing.T) {
	mgr := NewManager()
	stringsMap := mgr.GetAllStrings("en")
	if len(stringsMap) == 0 {
		t.Fatalf("expected non-empty strings map for en")
	}
	if stringsMap["menu.add"] != "Add entry" {
		t.Errorf("expected 'Add entry', got '%s'", stringsMap["menu.add"])
	}
	if stringsMap["common.save"] != "Save" {
		t.Errorf("expected 'Save', got '%s'", stringsMap["common.save"])
	}
}
