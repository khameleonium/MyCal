package web

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"mycalendar/calendar"
	"mycalendar/config"
	"mycalendar/models"
)

func newTestServer(t *testing.T) (*Server, *calendar.Service, string) {
	t.Helper()
	dir := t.TempDir()
	svc, err := calendar.NewService(context.Background(), dir, "mycal", models.SplitNone)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	cfg := config.DefaultConfig()
	cfgPath := filepath.Join(dir, config.ConfigFile)

	// Add sample seed sessions
	_ = svc.Add(models.Session{
		ID:       "20261104100000",
		Time:     "10:00",
		Name:     "Утренняя тренировка",
		Type:     "Стрельба из лука",
		Duration: 60,
		Status:   "Активно",
		Notes:    "Тестовая запись",
	})
	_ = svc.Add(models.Session{
		ID:       "20261104140000",
		Time:     "14:00",
		Name:     "Метание",
		Type:     "Метание ножей",
		Duration: 45,
		Status:   "Пропущено",
		Notes:    "",
	})
	_ = svc.Save(context.Background())

	srv := NewServer(svc, cfg, cfgPath, "v2.5.0-test")
	return srv, svc, dir
}

func TestStaticFrontendServed(t *testing.T) {
	srv, _, _ := newTestServer(t)
	handler := srv.Routes()

	// 1. Index HTML
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /: got status %d, want 200", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("MyCal Web Dashboard")) {
		t.Errorf("GET /: response does not contain title")
	}

	// 2. CSS
	req = httptest.NewRequest(http.MethodGet, "/style.css", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /style.css: got status %d, want 200", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("--bg-app")) {
		t.Errorf("GET /style.css: response does not contain CSS variables")
	}

	// 3. JS
	req = httptest.NewRequest(http.MethodGet, "/app.js", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /app.js: got status %d, want 200", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("selectNewStatus")) {
		t.Errorf("GET /app.js: response does not contain client functions")
	}
}

func TestGetSessionsAPI(t *testing.T) {
	srv, _, _ := newTestServer(t)
	handler := srv.Routes()

	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/sessions: got %d", rec.Code)
	}

	var res struct {
		Sessions     []models.Session `json:"sessions"`
		Count        int              `json:"count"`
		TotalMinutes int              `json:"total_minutes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("json unmarshal: %v", err)
	}

	if res.Count != 2 {
		t.Errorf("expected 2 sessions, got %d", res.Count)
	}
	if res.TotalMinutes != 105 {
		t.Errorf("expected 105 minutes, got %d", res.TotalMinutes)
	}
}

func TestRangeAndRefQueryAPI(t *testing.T) {
	srv, _, _ := newTestServer(t)
	handler := srv.Routes()

	// 1. InRange match
	req := httptest.NewRequest(http.MethodGet, "/api/sessions?start=2026-11-01&end=2026-11-10", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/sessions?start...: got %d", rec.Code)
	}
	var res1 struct {
		Count int `json:"count"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &res1)
	if res1.Count != 2 {
		t.Errorf("expected 2 sessions in range, got %d", res1.Count)
	}

	// 2. InRange outside
	req = httptest.NewRequest(http.MethodGet, "/api/sessions?start=2026-12-01&end=2026-12-31", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var res2 struct {
		Count int `json:"count"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &res2)
	if res2.Count != 0 {
		t.Errorf("expected 0 sessions in Dec range, got %d", res2.Count)
	}

	// 3. Month with ref
	req = httptest.NewRequest(http.MethodGet, "/api/sessions?period=month&ref=2026-11-15", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var res3 struct {
		Count int `json:"count"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &res3)
	if res3.Count != 2 {
		t.Errorf("expected 2 sessions for Nov ref, got %d", res3.Count)
	}
}

func TestQuickStatusUpdate(t *testing.T) {
	srv, svc, _ := newTestServer(t)
	handler := srv.Routes()

	body := bytes.NewBufferString(`{"status": "Отменено"}`)
	req := httptest.NewRequest(http.MethodPatch, "/api/sessions/20261104100000/status", body)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH status: got %d, body: %s", rec.Code, rec.Body.String())
	}

	sess, ok := svc.ByID("20261104100000")
	if !ok || sess.Status != "Отменено" {
		t.Errorf("status not updated on disk: got %s, want Отменено", sess.Status)
	}
}

func TestQuickFieldUpdate(t *testing.T) {
	srv, svc, _ := newTestServer(t)
	handler := srv.Routes()

	// Update name
	body := bytes.NewBufferString(`{"field": "name", "value": "Новая стрельба"}`)
	req := httptest.NewRequest(http.MethodPatch, "/api/sessions/20261104100000/field", body)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH field: got %d", rec.Code)
	}

	sess, _ := svc.ByID("20261104100000")
	if sess.Name != "Новая стрельба" {
		t.Errorf("name not updated: got %s", sess.Name)
	}
}

func TestCreateAndRepeatSessionAPI(t *testing.T) {
	srv, svc, _ := newTestServer(t)
	handler := srv.Routes()

	payload := `{
		"date": "2026-11-05",
		"time": "15:30",
		"name": "Новая серия",
		"type": "Стрельба из лука",
		"duration": 45,
		"notes": "тест серии",
		"status": "Активно",
		"repeat": "d",
		"repeat_until": "2026-11-07"
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/sessions", bytes.NewBufferString(payload))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /api/sessions: got %d, body: %s", rec.Code, rec.Body.String())
	}

	var res struct {
		Ok       bool   `json:"ok"`
		Count    int    `json:"count"`
		SeriesID string `json:"series_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}

	if !res.Ok || res.Count != 3 {
		t.Errorf("expected 3 sessions in daily series 05->07, got %d", res.Count)
	}

	series := svc.BySeries(res.SeriesID)
	if len(series) != 3 {
		t.Errorf("expected 3 sessions in service, got %d", len(series))
	}
}

func TestDeleteSessionAPI(t *testing.T) {
	srv, svc, _ := newTestServer(t)
	handler := srv.Routes()

	req := httptest.NewRequest(http.MethodDelete, "/api/sessions/20261104140000", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE: got %d", rec.Code)
	}

	if _, ok := svc.ByID("20261104140000"); ok {
		t.Errorf("session was not deleted")
	}
}

func TestDuplicateSessionAPI(t *testing.T) {
	srv, svc, _ := newTestServer(t)
	handler := srv.Routes()

	req := httptest.NewRequest(http.MethodPost, "/api/sessions/20261104100000/duplicate", bytes.NewBufferString(`{}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("duplicate: got %d", rec.Code)
	}

	var res struct {
		Ok      bool           `json:"ok"`
		Session models.Session `json:"session"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &res)

	if !res.Ok || res.Session.ID == "20261104100000" {
		t.Errorf("duplicate failed or generated identical ID: %+v", res.Session)
	}

	if svc.Count() != 3 {
		t.Errorf("expected 3 sessions, got %d", svc.Count())
	}
}

func TestCashAndBatchOperationsAPI(t *testing.T) {
	srv, svc, _ := newTestServer(t)
	handler := srv.Routes()

	// 1. Create with Cash "0001.0"
	body := bytes.NewBufferString(`{
		"date": "2026-11-04",
		"time": "18:00",
		"name": "Платная тренировка",
		"type": "Стрельба",
		"duration": 60,
		"cash": "0001.0"
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions", body)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /api/sessions: got %d", rec.Code)
	}
	var created struct {
		Session models.Session `json:"session"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if created.Session.Cash != 1.0 {
		t.Errorf("expected cash 1.0 from '0001.0', got %v", created.Session.Cash)
	}

	// 2. Batch update field "cash"
	batchBody := bytes.NewBufferString(`{
		"ids": ["20261104100000", "20261104140000"],
		"field": "cash",
		"value": "00250.5"
	}`)
	req = httptest.NewRequest(http.MethodPatch, "/api/sessions/batch", batchBody)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("batch PATCH: got %d", rec.Code)
	}

	s1, _ := svc.ByID("20261104100000")
	s2, _ := svc.ByID("20261104140000")
	if s1.Cash != 250.5 || s2.Cash != 250.5 {
		t.Errorf("batch cash update failed: s1=%v, s2=%v", s1.Cash, s2.Cash)
	}

	// 3. Batch delete
	delBody := bytes.NewBufferString(`{
		"ids": ["20261104100000", "20261104140000"]
	}`)
	req = httptest.NewRequest(http.MethodDelete, "/api/sessions/batch", delBody)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("batch DELETE: got %d", rec.Code)
	}
	if _, ok := svc.ByID("20261104100000"); ok {
		t.Errorf("session 1 was not batch deleted")
	}
}

func TestTypesAndStatusesAPI(t *testing.T) {
	server, _, _ := newTestServer(t)
	handler := server.Routes()

	// 1. Initial types
	req := httptest.NewRequest(http.MethodGet, "/api/types", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/types: got %d", rec.Code)
	}

	// 2. Add custom type
	body := bytes.NewBufferString(`{"types": ["Шахматы", "Йога"]}`)
	req = httptest.NewRequest(http.MethodPost, "/api/types", body)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/types: got %d", rec.Code)
	}
	if !containsStr(server.svc.AvailableTypes(server.cfg.CustomTypes, server.cfg.ExcludedTypes), "Шахматы") {
		t.Errorf("expected 'Шахматы' in available types")
	}

	// 3. Delete type
	req = httptest.NewRequest(http.MethodDelete, "/api/types/Шахматы", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE /api/types/Шахматы: got %d", rec.Code)
	}
	if containsStr(server.svc.AvailableTypes(server.cfg.CustomTypes, server.cfg.ExcludedTypes), "Шахматы") {
		t.Errorf("expected 'Шахматы' to be excluded from available types")
	}

	// 4. Add custom status
	stBody := bytes.NewBufferString(`{"statuses": ["В ожидании"]}`)
	req = httptest.NewRequest(http.MethodPost, "/api/statuses", stBody)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/statuses: got %d", rec.Code)
	}
	if !containsStr(server.svc.AvailableStatuses(server.cfg.CustomStatuses, server.cfg.ExcludedStatuses), "В ожидании") {
		t.Errorf("expected 'В ожидании' in available statuses")
	}

	// 5. Delete status
	req = httptest.NewRequest(http.MethodDelete, "/api/statuses/В%20ожидании", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE /api/statuses: got %d", rec.Code)
	}
	if containsStr(server.svc.AvailableStatuses(server.cfg.CustomStatuses, server.cfg.ExcludedStatuses), "В ожидании") {
		t.Errorf("expected 'В ожидании' to be excluded from available statuses")
	}
}

func containsStr(arr []string, s string) bool {
	for _, x := range arr {
		if strings.EqualFold(x, s) {
			return true
		}
	}
	return false
}
