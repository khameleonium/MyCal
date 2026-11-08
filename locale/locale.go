package locale

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

//go:embed locales/*.json
var embeddedLocales embed.FS

// Meta holds localization file header information.
type Meta struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Author      string `json:"author,omitempty"`
	Description string `json:"description,omitempty"`
}

// LangInfo describes an available language in the system.
type LangInfo struct {
	Code     string `json:"code"`
	Name     string `json:"name"`
	Author   string `json:"author,omitempty"`
	IsCustom bool   `json:"is_custom"`
}

// Bundle represents one loaded language dictionary.
type Bundle struct {
	Meta    Meta
	Strings map[string]string
	Raw     map[string]any // Original unflattened map for templating
}

// Manager coordinates embedded and external localization bundles.
type Manager struct {
	mu          sync.RWMutex
	currentLang string
	localePath  string
	bundles     map[string]*Bundle
}

// filenameRegex matches mycal_locale--<lang>.json
var filenameRegex = regexp.MustCompile(`^mycal_locale--([a-zA-Z0-9_\-]+)\.json$`)

// NewManager creates a new localization manager loaded with embedded locales.
func NewManager() *Manager {
	m := &Manager{
		currentLang: "ru",
		bundles:     make(map[string]*Bundle),
	}
	m.loadEmbedded()
	return m
}

// loadEmbedded loads all mycal_locale--*.json files embedded in the binary.
func (m *Manager) loadEmbedded() {
	entries, err := embeddedLocales.ReadDir("locales")
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		matches := filenameRegex.FindStringSubmatch(entry.Name())
		if len(matches) < 2 {
			continue
		}
		data, err := embeddedLocales.ReadFile("locales/" + entry.Name())
		if err != nil {
			continue
		}
		fallbackCode := matches[1]
		b, err := parseBundleData(data, fallbackCode)
		if err == nil && b != nil {
			m.bundles[b.Meta.Code] = b
		}
	}
}

// parseBundleData parses raw JSON bytes into a Bundle, extracting _meta and flattening keys.
func parseBundleData(data []byte, fallbackCode string) (*Bundle, error) {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	meta := Meta{
		Code: fallbackCode,
		Name: strings.ToUpper(fallbackCode),
	}

	if mVal, ok := raw["_meta"].(map[string]any); ok {
		if c, ok := mVal["code"].(string); ok && strings.TrimSpace(c) != "" {
			meta.Code = strings.TrimSpace(strings.ToLower(c))
		}
		if n, ok := mVal["name"].(string); ok && strings.TrimSpace(n) != "" {
			meta.Name = strings.TrimSpace(n)
		}
		if a, ok := mVal["author"].(string); ok {
			meta.Author = strings.TrimSpace(a)
		}
		if d, ok := mVal["description"].(string); ok {
			meta.Description = strings.TrimSpace(d)
		}
	}

	stringsMap := make(map[string]string)
	for k, v := range raw {
		if k == "_meta" {
			continue
		}
		flatten(k, v, stringsMap)
	}

	return &Bundle{
		Meta:    meta,
		Strings: stringsMap,
		Raw:     raw,
	}, nil
}

// flatten recursively converts nested JSON structures into dot-separated keys.
func flatten(prefix string, value any, out map[string]string) {
	switch v := value.(type) {
	case map[string]any:
		for k, child := range v {
			next := k
			if prefix != "" {
				next = prefix + "." + k
			}
			flatten(next, child, out)
		}
	case string:
		out[prefix] = v
	case float64:
		out[prefix] = strconv.FormatFloat(v, 'f', -1, 64)
	case int:
		out[prefix] = strconv.Itoa(v)
	case bool:
		out[prefix] = strconv.FormatBool(v)
	}
}

// LoadExternal scans executable directory, current working directory, and custom path
// for mycal_locale--*.json files. Existing languages are merged (external values take precedence);
// new languages are added to the available list.
func (m *Manager) LoadExternal(customPath string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.localePath = customPath

	// Re-initialize bundles from embedded base first to allow clean external re-scans
	m.bundles = make(map[string]*Bundle)
	m.loadEmbedded()

	var dirsToScan []string

	// 1. Executable directory
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		dirsToScan = append(dirsToScan, exeDir)
	}

	// 2. Current working directory
	if cwd, err := os.Getwd(); err == nil {
		dirsToScan = append(dirsToScan, cwd)
	}

	// 3. Custom path from settings (directory or single file)
	if strings.TrimSpace(customPath) != "" {
		expanded := expandHome(strings.TrimSpace(customPath))
		if fi, err := os.Stat(expanded); err == nil {
			if fi.IsDir() {
				dirsToScan = append(dirsToScan, expanded)
			} else {
				// Single file specified
				m.loadExternalFile(expanded)
			}
		}
	}

	// Deduplicate directories
	seenDirs := make(map[string]bool)
	for _, dir := range dirsToScan {
		abs, err := filepath.Abs(dir)
		if err != nil || seenDirs[abs] {
			continue
		}
		seenDirs[abs] = true
		m.scanDirectory(abs)
	}
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") || path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			if path == "~" {
				return home
			}
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

// scanDirectory reads all mycal_locale--*.json files in a directory.
func (m *Manager) scanDirectory(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if filenameRegex.MatchString(entry.Name()) {
			m.loadExternalFile(filepath.Join(dir, entry.Name()))
		}
	}
}

// loadExternalFile parses an external file and merges it into bundles.
func (m *Manager) loadExternalFile(filePath string) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return
	}
	base := filepath.Base(filePath)
	matches := filenameRegex.FindStringSubmatch(base)
	fallbackCode := ""
	if len(matches) >= 2 {
		fallbackCode = matches[1]
	}

	b, err := parseBundleData(data, fallbackCode)
	if err != nil || b == nil {
		return
	}

	code := b.Meta.Code
	if existing, found := m.bundles[code]; found {
		// Merge: overwrite existing strings with external ones
		for k, v := range b.Strings {
			existing.Strings[k] = v
		}
		if b.Meta.Name != "" && b.Meta.Name != strings.ToUpper(code) {
			existing.Meta.Name = b.Meta.Name
		}
		if b.Meta.Author != "" {
			existing.Meta.Author = b.Meta.Author
		}
		if b.Meta.Description != "" {
			existing.Meta.Description = b.Meta.Description
		}
		if b.Raw != nil {
			existing.Raw = b.Raw
		}
	} else {
		m.bundles[code] = b
	}
}

// SetLanguage changes the current active language.
func (m *Manager) SetLanguage(lang string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	lang = strings.TrimSpace(strings.ToLower(lang))
	if lang == "" {
		lang = "ru"
	}
	m.currentLang = lang
}

// CurrentLanguage returns the current active language code.
func (m *Manager) CurrentLanguage() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.currentLang == "" {
		return "ru"
	}
	return m.currentLang
}

// AvailableLanguages returns a list of all loaded languages.
// Russian (ru) is listed first, English (en) second, then remaining languages alphabetically.
func (m *Manager) AvailableLanguages() []LangInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []LangInfo
	for code, bundle := range m.bundles {
		isCustom := code != "ru" && code != "en"
		result = append(result, LangInfo{
			Code:     code,
			Name:     bundle.Meta.Name,
			Author:   bundle.Meta.Author,
			IsCustom: isCustom,
		})
	}

	sort.Slice(result, func(i, j int) bool {
		cI, cJ := result[i].Code, result[j].Code
		if cI == "ru" {
			return true
		}
		if cJ == "ru" {
			return false
		}
		if cI == "en" {
			return true
		}
		if cJ == "en" {
			return false
		}
		return result[i].Name < result[j].Name
	})

	return result
}

// T translates a key using the active language with fallbacks:
// currentLang -> ru -> en -> key.
// If formatting args are provided, fmt.Sprintf is applied.
func (m *Manager) T(key string, args ...any) string {
	return m.TDef(key, key, args...)
}

// TDef translates a key using active language with fallbacks:
// currentLang -> ru -> en -> defaultVal.
func (m *Manager) TDef(key string, defaultVal string, args ...any) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	lang := m.currentLang
	if lang == "" {
		lang = "ru"
	}

	val := ""
	// 1. Try active language
	if bundle, ok := m.bundles[lang]; ok {
		if s, found := bundle.Strings[key]; found && strings.TrimSpace(s) != "" {
			val = s
		}
	}

	// 2. Try Russian fallback
	if val == "" && lang != "ru" {
		if bundle, ok := m.bundles["ru"]; ok {
			if s, found := bundle.Strings[key]; found && strings.TrimSpace(s) != "" {
				val = s
			}
		}
	}

	// 3. Try English fallback
	if val == "" && lang != "en" {
		if bundle, ok := m.bundles["en"]; ok {
			if s, found := bundle.Strings[key]; found && strings.TrimSpace(s) != "" {
				val = s
			}
		}
	}

	if val == "" {
		val = defaultVal
	}

	if len(args) > 0 {
		return fmt.Sprintf(val, args...)
	}
	return val
}

// GetAllStrings returns a complete key-value dictionary for the requested language,
// with missing keys filled from fallbacks (ru, then en).
func (m *Manager) GetAllStrings(lang string) map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	lang = strings.TrimSpace(strings.ToLower(lang))
	if lang == "" {
		lang = m.currentLang
	}
	if lang == "" {
		lang = "ru"
	}

	out := make(map[string]string)

	// Base fill from en
	if en, ok := m.bundles["en"]; ok {
		for k, v := range en.Strings {
			out[k] = v
		}
	}

	// Base fill from ru (overrides en)
	if ru, ok := m.bundles["ru"]; ok {
		for k, v := range ru.Strings {
			out[k] = v
		}
	}

	// Language overrides
	if target, ok := m.bundles[lang]; ok && lang != "ru" {
		for k, v := range target.Strings {
			out[k] = v
		}
	}

	return out
}

// GetTemplateJSON generates formatted JSON bytes for a template localization file
// based on baseLang (defaults to "ru" if empty).
func (m *Manager) GetTemplateJSON(baseLang, newCode, newName string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	baseLang = strings.TrimSpace(strings.ToLower(baseLang))
	if baseLang == "" {
		baseLang = "ru"
	}
	bundle, ok := m.bundles[baseLang]
	if !ok {
		bundle = m.bundles["ru"]
	}
	if bundle == nil {
		return nil, fmt.Errorf("base locale %s not found", baseLang)
	}

	// Deep clone raw map or create structured map
	outMap := make(map[string]any)
	if bundle.Raw != nil {
		for k, v := range bundle.Raw {
			outMap[k] = v
		}
	} else {
		// Fallback: unflatten Strings
		for k, v := range bundle.Strings {
			setNestedKey(outMap, k, v)
		}
	}

	if newCode == "" {
		newCode = "custom"
	}
	if newName == "" {
		newName = "Custom Language"
	}

	outMap["_meta"] = map[string]any{
		"code":        newCode,
		"name":        newName,
		"author":      "Your Name <optional>",
		"description": fmt.Sprintf("Custom localization for MyCal based on %s", baseLang),
	}

	return json.MarshalIndent(outMap, "", "  ")
}

func setNestedKey(m map[string]any, key string, val string) {
	parts := strings.Split(key, ".")
	cur := m
	for i := 0; i < len(parts)-1; i++ {
		p := parts[i]
		if _, ok := cur[p]; !ok {
			cur[p] = make(map[string]any)
		}
		if next, ok := cur[p].(map[string]any); ok {
			cur = next
		} else {
			return
		}
	}
	cur[parts[len(parts)-1]] = val
}

// ExportTemplate generates and writes a template localization file to targetPath.
func (m *Manager) ExportTemplate(baseLang, targetPath, newCode, newName string) error {
	data, err := m.GetTemplateJSON(baseLang, newCode, newName)
	if err != nil {
		return err
	}
	targetPath = expandHome(strings.TrimSpace(targetPath))
	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		return err
	}
	return os.WriteFile(targetPath, data, 0644)
}

// Global default instance and convenience wrappers.
var (
	defaultMgr  *Manager
	defaultOnce sync.Once
)

// Default returns the process-wide localization manager singleton.
func Default() *Manager {
	defaultOnce.Do(func() {
		defaultMgr = NewManager()
	})
	return defaultMgr
}

// Init initializes the default manager with custom path and language.
func Init(customPath, activeLang string) {
	mgr := Default()
	mgr.LoadExternal(customPath)
	if activeLang != "" {
		mgr.SetLanguage(activeLang)
	}
}

// T translates a key using the process-wide manager.
func T(key string, args ...any) string {
	return Default().T(key, args...)
}

// TDef translates a key with a default value fallback.
func TDef(key string, defaultVal string, args ...any) string {
	return Default().TDef(key, defaultVal, args...)
}

// SetLanguage sets the active language on the default manager.
func SetLanguage(lang string) {
	Default().SetLanguage(lang)
}

// CurrentLanguage returns the current language code.
func CurrentLanguage() string {
	return Default().CurrentLanguage()
}

// AvailableLanguages returns available languages.
func AvailableLanguages() []LangInfo {
	return Default().AvailableLanguages()
}

// ExportTemplate exports a translation template using the default manager.
func ExportTemplate(baseLang, targetPath, newCode, newName string) error {
	return Default().ExportTemplate(baseLang, targetPath, newCode, newName)
}

// GetTemplateJSON returns template JSON bytes.
func GetTemplateJSON(baseLang, newCode, newName string) ([]byte, error) {
	return Default().GetTemplateJSON(baseLang, newCode, newName)
}

// GetAllStrings returns strings for a language.
func GetAllStrings(lang string) map[string]string {
	return Default().GetAllStrings(lang)
}
