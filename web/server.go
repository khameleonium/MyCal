package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"mycalendar/calendar"
	"mycalendar/config"
	"mycalendar/models"
	"mycalendar/parser"
	"mycalendar/service"
)

// Server coordinates the web dashboard, providing a REST API and static frontend.
type Server struct {
	svc     *calendar.Service
	cfg     *models.Config
	cfgPath string
	version string
	mu      sync.RWMutex
}

// NewServer creates a new web Server instance.
func NewServer(svc *calendar.Service, cfg *models.Config, cfgPath string, version string) *Server {
	if version == "" {
		version = "dev"
	}
	return &Server{
		svc:     svc,
		cfg:     cfg,
		cfgPath: cfgPath,
		version: version,
	}
}

// Start launches the HTTP server, listening on 127.0.0.1:port (or next free port).
func (s *Server) Start(ctx context.Context, defaultPort int, shouldOpenBrowser bool) error {
	listener, port, err := s.listenOnAvailablePort(defaultPort)
	if err != nil {
		return fmt.Errorf("не удалось запустить сервер: %w", err)
	}

	url := fmt.Sprintf("http://127.0.0.1:%d", port)
	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════════════════════════════╗")
	fmt.Printf("║  📅  MyCal Web Dashboard запущен: %-34s ║\n", url)
	fmt.Println("║  Для выхода нажмите Ctrl+C                                           ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════════════╝")
	fmt.Println()

	if shouldOpenBrowser {
		go func() {
			time.Sleep(350 * time.Millisecond)
			openBrowser(url)
		}()
	}

	httpServer := &http.Server{
		Handler: s.Routes(),
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	err = httpServer.Serve(listener)
	if err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (s *Server) listenOnAvailablePort(startPort int) (net.Listener, int, error) {
	if startPort <= 0 {
		startPort = 8080
	}
	for p := startPort; p < startPort+100; p++ {
		addr := fmt.Sprintf("127.0.0.1:%d", p)
		l, err := net.Listen("tcp", addr)
		if err == nil {
			return l, p, nil
		}
	}
	// Fallback to random system port
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, 0, err
	}
	tcpAddr := l.Addr().(*net.TCPAddr)
	return l, tcpAddr.Port, nil
}

// Routes constructs the HTTP multiplexer with API and static routes.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	// REST API endpoints
	mux.HandleFunc("/api/sessions/batch", s.handleSessionsBatch)
	mux.HandleFunc("/api/sessions", s.handleSessions)
	mux.HandleFunc("/api/sessions/", s.handleSessionByID)
	mux.HandleFunc("/api/types", s.handleTypes)
	mux.HandleFunc("/api/statuses", s.handleStatuses)
	mux.HandleFunc("/api/stats", s.handleStats)
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/info", s.handleInfo)
	mux.HandleFunc("/api/service/status", s.handleServiceStatus)
	mux.HandleFunc("/api/service/action", s.handleServiceAction)

	// Embedded Static Frontend
	mux.Handle("/", StaticHandler())

	return s.corsMiddleware(mux)
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// --------------------------- REST API Handlers ---------------------------

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.getSessions(w, r)
	case http.MethodPost:
		s.createSession(w, r)
	default:
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
	}
}

func (s *Server) getSessions(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	q := r.URL.Query()
	period := q.Get("period")
	startStr := q.Get("start")
	endStr := q.Get("end")
	typeFilter := q.Get("type")
	searchQuery := q.Get("q")
	dateStr := q.Get("date")

	now := time.Now()
	ref := now
	if refStr := q.Get("ref"); refStr != "" {
		if t, err := time.Parse("2006-01-02", refStr); err == nil {
			ref = t
		}
	}

	var sessions []models.Session

	switch {
	case startStr != "" && endStr != "":
		start, err1 := time.Parse("2006-01-02", startStr)
		end, err2 := time.Parse("2006-01-02", endStr)
		if err1 == nil && err2 == nil {
			if start.After(end) {
				start, end = end, start
			}
			sessions = s.svc.InRange(start, end)
		} else {
			sessions = s.svc.All()
		}
	case dateStr != "":
		d, err := time.Parse("2006-01-02", dateStr)
		if err == nil {
			sessions = s.svc.Day(d)
		} else {
			sessions = s.svc.All()
		}
	case period == "today":
		sessions = s.svc.Day(now)
	case period == "week":
		sessions = s.svc.Week(ref)
	case period == "month":
		sessions = s.svc.Month(ref)
	case period == "all":
		sessions = s.svc.All()
	default:
		// Default to all
		sessions = s.svc.All()
	}

	// Filter by type if requested
	if typeFilter != "" {
		types := strings.Split(typeFilter, ",")
		var filtered []models.Session
		for _, sess := range sessions {
			for _, t := range types {
				t = strings.TrimSpace(t)
				if strings.EqualFold(sess.Type, t) || strings.Contains(strings.ToLower(sess.Type), strings.ToLower(t)) {
					filtered = append(filtered, sess)
					break
				}
			}
		}
		sessions = filtered
	}

	// Filter by search text query if requested
	if searchQuery != "" {
		var matched []models.Session
		for _, sess := range sessions {
			if calendar.Matches(sess, searchQuery) {
				matched = append(matched, sess)
			}
		}
		sessions = matched
	}

	totalMin := calendar.TotalMinutes(sessions)
	totalCash := calendar.TotalCash(sessions)
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"sessions":      sessions,
		"count":         len(sessions),
		"total_minutes": totalMin,
		"total_hours":   float64(totalMin) / 60.0,
		"total_cash":    totalCash,
	})
}

type createSessionRequest struct {
	Date        string      `json:"date"` // YYYY-MM-DD or DD.MM.YYYY
	Time        string      `json:"time"` // HH:MM
	Name        string      `json:"name"`
	Type        string      `json:"type"`
	Duration    int         `json:"duration"` // in minutes
	Notes       string      `json:"notes"`
	Status      string      `json:"status"`
	Cash        interface{} `json:"cash"`
	Repeat      string      `json:"repeat"`       // none, d, w, m
	RepeatUntil string      `json:"repeat_until"` // YYYY-MM-DD or DD.MM.YYYY
}

func (s *Server) createSession(w http.ResponseWriter, r *http.Request) {
	var req createSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Некорректный JSON запрос: "+err.Error())
		return
	}

	date, err := parseDateLenient(req.Date)
	if err != nil {
		respondError(w, http.StatusBadRequest, "Неверный формат даты: "+err.Error())
		return
	}

	tm, err := parser.ParseTime(req.Time)
	if err != nil {
		respondError(w, http.StatusBadRequest, "Неверный формат времени: "+err.Error())
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		respondError(w, http.StatusBadRequest, "Наименование записи не может быть пустым")
		return
	}

	dur := req.Duration
	if dur <= 0 {
		dur = s.cfg.DefaultDuration
		if dur <= 0 {
			dur = 60
		}
	}

	base := models.Session{
		Time:     tm.Format("15:04"),
		Name:     name,
		Type:     strings.TrimSpace(req.Type),
		Duration: dur,
		Notes:    strings.TrimSpace(req.Notes),
		Status:   strings.TrimSpace(req.Status),
		Cash:     parseCash(req.Cash),
	}

	dates := []time.Time{date}
	if req.Repeat != "" && req.Repeat != "none" {
		until, errUntil := parseDateLenient(req.RepeatUntil)
		if errUntil != nil || until.Before(date) {
			until = date.AddDate(0, 1, 0) // default 1 month if missing
		}
		occurrences, _ := calendar.RepeatDates(date, until, req.Repeat)
		dates = append(dates, occurrences...)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	first := base
	first.ID = s.svc.GenerateID(date, tm)
	seriesID := ""
	if len(dates) > 1 {
		seriesID = first.ID
		first.SeriesID = seriesID
	}

	toAdd := []models.Session{first}
	for _, d := range dates[1:] {
		occ := base
		occ.ID = s.svc.GenerateID(d, tm)
		occ.SeriesID = seriesID
		toAdd = append(toAdd, occ)
	}

	added, err := s.svc.AddBatch(toAdd)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Ошибка сохранения записей: "+err.Error())
		return
	}

	if err := s.svc.Save(context.Background()); err != nil {
		respondError(w, http.StatusInternalServerError, "Не удалось записать файл данных: "+err.Error())
		return
	}

	respondJSON(w, http.StatusCreated, map[string]interface{}{
		"ok":        true,
		"count":     added,
		"first_id":  first.ID,
		"series_id": seriesID,
		"session":   first,
	})
}

func (s *Server) handleSessionByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
	parts := strings.Split(path, "/")
	id := parts[0]
	if id == "" {
		http.Error(w, "ID записи не указан", http.StatusBadRequest)
		return
	}

	// Sub-routes: /api/sessions/{id}/status, /api/sessions/{id}/field, /api/sessions/{id}/duplicate
	if len(parts) > 1 {
		action := parts[1]
		switch action {
		case "status":
			s.updateStatus(w, r, id)
			return
		case "field":
			s.updateField(w, r, id)
			return
		case "duplicate":
			s.duplicateSession(w, r, id)
			return
		}
	}

	switch r.Method {
	case http.MethodGet:
		s.getSessionByID(w, id)
	case http.MethodPut:
		s.updateSession(w, r, id)
	case http.MethodDelete:
		s.deleteSession(w, r, id)
	default:
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
	}
}

func (s *Server) getSessionByID(w http.ResponseWriter, id string) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sess, ok := s.svc.ByID(id)
	if !ok {
		respondError(w, http.StatusNotFound, "Запись не найдена")
		return
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"session": sess,
		"series":  s.svc.BySeries(sess.SeriesID),
	})
}

type updateSessionRequest struct {
	Date         string      `json:"date"`
	Time         *string     `json:"time"`
	Name         *string     `json:"name"`
	Type         *string     `json:"type"`
	Duration     *int        `json:"duration"`
	Notes        *string     `json:"notes"`
	Status       *string     `json:"status"`
	Cash         interface{} `json:"cash"`
	UpdateSeries bool        `json:"update_series"`
}

func (s *Server) updateSession(w http.ResponseWriter, r *http.Request, id string) {
	var req updateSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Некорректный JSON запрос: "+err.Error())
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	sess, ok := s.svc.ByID(id)
	if !ok {
		respondError(w, http.StatusNotFound, "Запись с таким ID не найдена")
		return
	}

	var cashPtr *float64
	if req.Cash != nil {
		c := parseCash(req.Cash)
		cashPtr = &c
	}

	patch := calendar.Patch{
		Time:     req.Time,
		Name:     req.Name,
		Type:     req.Type,
		Duration: req.Duration,
		Notes:    req.Notes,
		Status:   req.Status,
		Cash:     cashPtr,
	}

	// If date changed for a single session, we need to rebuild ID
	if req.Date != "" && req.Date != sess.Date() && !req.UpdateSeries {
		newDate, err := parseDateLenient(req.Date)
		if err == nil {
			tStr := sess.Time
			if req.Time != nil {
				tStr = *req.Time
			}
			tm, _ := parser.ParseTime(tStr)
			newID := s.svc.GenerateID(newDate, tm)

			// Update in-place
			_ = s.svc.Delete(id)
			sess.ID = newID
			if req.Time != nil {
				sess.Time = *req.Time
			}
			if req.Name != nil {
				sess.Name = *req.Name
			}
			if req.Type != nil {
				sess.Type = *req.Type
			}
			if req.Duration != nil {
				sess.Duration = *req.Duration
			}
			if req.Notes != nil {
				sess.Notes = *req.Notes
			}
			if req.Status != nil {
				sess.Status = *req.Status
			}
			if cashPtr != nil {
				sess.Cash = *cashPtr
			}
			_ = s.svc.Add(sess)
			_ = s.svc.Save(context.Background())
			respondJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "session": sess})
			return
		}
	}

	if req.UpdateSeries && sess.IsRepeat() {
		n := s.svc.UpdateSeries(sess.SeriesID, patch)
		_ = s.svc.Save(context.Background())
		respondJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "updated_count": n})
		return
	}

	if err := s.svc.Update(id, patch); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	_ = s.svc.Save(context.Background())
	updated, _ := s.svc.ByID(id)
	respondJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "session": updated})
}

func (s *Server) updateStatus(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPatch && r.Method != http.MethodPost {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Некорректный JSON запрос")
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	status := strings.TrimSpace(req.Status)
	patch := calendar.Patch{Status: &status}
	if err := s.svc.Update(id, patch); err != nil {
		respondError(w, http.StatusNotFound, "Запись не найдена")
		return
	}

	_ = s.svc.Save(context.Background())
	updated, _ := s.svc.ByID(id)
	respondJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "session": updated})
}

func (s *Server) updateField(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPatch && r.Method != http.MethodPost {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Field string      `json:"field"`
		Value interface{} `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Некорректный JSON запрос")
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var patch calendar.Patch
	switch strings.ToLower(req.Field) {
	case "name":
		v := fmt.Sprintf("%v", req.Value)
		patch.Name = &v
	case "time":
		v := fmt.Sprintf("%v", req.Value)
		patch.Time = &v
	case "type":
		v := fmt.Sprintf("%v", req.Value)
		patch.Type = &v
	case "notes":
		v := fmt.Sprintf("%v", req.Value)
		patch.Notes = &v
	case "status":
		v := fmt.Sprintf("%v", req.Value)
		patch.Status = &v
	case "cash":
		c := parseCash(req.Value)
		patch.Cash = &c
	case "duration":
		if d, ok := req.Value.(float64); ok {
			dur := int(d)
			patch.Duration = &dur
		} else if sVal, ok := req.Value.(string); ok {
			if dur, err := strconv.Atoi(sVal); err == nil {
				patch.Duration = &dur
			}
		}
	default:
		respondError(w, http.StatusBadRequest, "Неизвестное поле: "+req.Field)
		return
	}

	if err := s.svc.Update(id, patch); err != nil {
		respondError(w, http.StatusNotFound, "Запись не найдена")
		return
	}

	_ = s.svc.Save(context.Background())
	updated, _ := s.svc.ByID(id)
	respondJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "session": updated})
}

func (s *Server) deleteSession(w http.ResponseWriter, r *http.Request, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess, ok := s.svc.ByID(id)
	if !ok {
		respondError(w, http.StatusNotFound, "Запись не найдена")
		return
	}

	deleteSeries := r.URL.Query().Get("series") == "true"
	if deleteSeries && sess.IsRepeat() {
		count := s.svc.DeleteSeries(sess.SeriesID)
		_ = s.svc.Save(context.Background())
		respondJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "deleted": count, "series": true})
		return
	}

	if !s.svc.Delete(id) {
		respondError(w, http.StatusNotFound, "Запись не найдена")
		return
	}

	_ = s.svc.Save(context.Background())
	respondJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "deleted": 1})
}

func (s *Server) duplicateSession(w http.ResponseWriter, r *http.Request, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	orig, ok := s.svc.ByID(id)
	if !ok {
		respondError(w, http.StatusNotFound, "Исходная запись не найдена")
		return
	}

	var req struct {
		Date string `json:"date"`
		Time string `json:"time"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	d, _ := time.Parse("2006-01-02", orig.Date())
	if req.Date != "" {
		if parsed, errD := parseDateLenient(req.Date); errD == nil {
			d = parsed
		}
	}
	tStr := orig.Time
	if req.Time != "" {
		tStr = req.Time
	}
	tm, _ := parser.ParseTime(tStr)

	newID := s.svc.GenerateID(d, tm)
	dup := orig
	dup.ID = newID
	dup.Time = tm.Format("15:04")
	dup.SeriesID = "" // duplicates start as standalone

	if err := s.svc.Add(dup); err != nil {
		respondError(w, http.StatusInternalServerError, "Не удалось добавить копию: "+err.Error())
		return
	}

	_ = s.svc.Save(context.Background())
	respondJSON(w, http.StatusCreated, map[string]interface{}{"ok": true, "session": dup})
}

func (s *Server) handleTypes(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var seed []string
	if s.cfg.DefaultType != "" {
		seed = append(seed, s.cfg.DefaultType)
	}
	types := s.svc.Types(seed...)
	respondJSON(w, http.StatusOK, map[string]interface{}{"types": types})
}

func (s *Server) handleStatuses(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	statuses := s.svc.Statuses(s.cfg.CustomStatuses...)
	respondJSON(w, http.StatusOK, map[string]interface{}{"statuses": statuses})
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	all := s.svc.All()
	totalMin := 0
	totalCash := 0.0
	byTypeMin := map[string]int{}
	byTypeCount := map[string]int{}

	for _, s := range all {
		totalMin += s.Duration
		totalCash += s.Cash
		tName := s.Type
		if tName == "" {
			tName = "(без типа)"
		}
		byTypeMin[tName] += s.Duration
		byTypeCount[tName]++
	}

	type typeStat struct {
		Type    string  `json:"type"`
		Minutes int     `json:"minutes"`
		Hours   float64 `json:"hours"`
		Percent float64 `json:"percent"`
		Count   int     `json:"count"`
	}

	var breakdown []typeStat
	for t, mins := range byTypeMin {
		pct := 0.0
		if totalMin > 0 {
			pct = (float64(mins) / float64(totalMin)) * 100.0
		}
		breakdown = append(breakdown, typeStat{
			Type:    t,
			Minutes: mins,
			Hours:   float64(mins) / 60.0,
			Percent: pct,
			Count:   byTypeCount[t],
		})
	}

	sort.Slice(breakdown, func(i, j int) bool {
		return breakdown[i].Minutes > breakdown[j].Minutes
	})

	avgDur := 0.0
	if len(all) > 0 {
		avgDur = float64(totalMin) / float64(len(all))
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"total_count":   len(all),
		"total_minutes": totalMin,
		"total_hours":   float64(totalMin) / 60.0,
		"total_cash":    totalCash,
		"avg_duration":  avgDur,
		"breakdown":     breakdown,
	})
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch r.Method {
	case http.MethodGet:
		respondJSON(w, http.StatusOK, map[string]interface{}{"config": s.cfg})
	case http.MethodPut, http.MethodPost:
		var updated models.Config
		if err := json.NewDecoder(r.Body).Decode(&updated); err != nil {
			respondError(w, http.StatusBadRequest, "Некорректный JSON конфигурации")
			return
		}
		updated.Normalize()
		*s.cfg = updated
		if s.cfgPath != "" {
			_ = config.Save(context.Background(), s.cfgPath, s.cfg)
		}
		respondJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "config": s.cfg})
	default:
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleSessionsBatch(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch r.Method {
	case http.MethodDelete:
		var req struct {
			IDs []string `json:"ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, http.StatusBadRequest, "Некорректный JSON запрос")
			return
		}
		deleted := 0
		for _, id := range req.IDs {
			if s.svc.Delete(id) {
				deleted++
			}
		}
		_ = s.svc.Save(context.Background())
		respondJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "deleted_count": deleted})

	case http.MethodPatch, http.MethodPost:
		var req struct {
			IDs   []string    `json:"ids"`
			Field string      `json:"field"`
			Value interface{} `json:"value"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, http.StatusBadRequest, "Некорректный JSON запрос")
			return
		}
		if len(req.IDs) == 0 {
			respondError(w, http.StatusBadRequest, "Не выбрано ни одной записи")
			return
		}

		field := strings.ToLower(strings.TrimSpace(req.Field))
		updated := 0

		if field == "date" {
			newDateStr := fmt.Sprintf("%v", req.Value)
			targetDate, err := parseDateLenient(newDateStr)
			if err != nil {
				respondError(w, http.StatusBadRequest, "Неверный формат даты: "+err.Error())
				return
			}
			for _, id := range req.IDs {
				sess, ok := s.svc.ByID(id)
				if !ok {
					continue
				}
				tm, err := parser.ParseTime(sess.Time)
				if err != nil {
					tm, _ = parser.ParseTime("10:00")
				}
				s.svc.Delete(id)
				sess.ID = s.svc.GenerateID(targetDate, tm)
				_ = s.svc.Add(sess)
				updated++
			}
			_ = s.svc.Save(context.Background())
			respondJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "updated_count": updated})
			return
		}

		var patch calendar.Patch
		switch field {
		case "name":
			v := strings.TrimSpace(fmt.Sprintf("%v", req.Value))
			patch.Name = &v
		case "time":
			v := strings.TrimSpace(fmt.Sprintf("%v", req.Value))
			tm, err := parser.ParseTime(v)
			if err != nil {
				respondError(w, http.StatusBadRequest, "Неверный формат времени: "+err.Error())
				return
			}
			formatted := tm.Format("15:04")
			patch.Time = &formatted
		case "type":
			v := strings.TrimSpace(fmt.Sprintf("%v", req.Value))
			patch.Type = &v
		case "duration":
			dur, _ := strconv.Atoi(fmt.Sprintf("%v", req.Value))
			if dur <= 0 {
				dur = 60
			}
			patch.Duration = &dur
		case "notes":
			v := strings.TrimSpace(fmt.Sprintf("%v", req.Value))
			patch.Notes = &v
		case "status":
			v := strings.TrimSpace(fmt.Sprintf("%v", req.Value))
			patch.Status = &v
		case "cash":
			c := parseCash(req.Value)
			patch.Cash = &c
		default:
			respondError(w, http.StatusBadRequest, "Неизвестное поле для изменения: "+req.Field)
			return
		}

		for _, id := range req.IDs {
			if err := s.svc.Update(id, patch); err == nil {
				updated++
			}
		}
		_ = s.svc.Save(context.Background())
		respondJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "updated_count": updated})

	default:
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"app":           "MyCal",
		"version":       s.version,
		"data_dir":      s.svc.DataDir(),
		"total_records": s.svc.Count(),
	})
}

// --------------------------- Helper Functions ---------------------------

func parseDateLenient(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Now(), nil
	}
	// Try standard ISO
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}
	// Try parser.ParseDate
	return parser.ParseDate(s, time.Now())
}

func parseCash(v interface{}) float64 {
	if v == nil {
		return 0.0
	}
	switch val := v.(type) {
	case float64:
		return val
	case float32:
		return float64(val)
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case json.Number:
		f, err := val.Float64()
		if err == nil {
			return f
		}
		return 0.0
	case string:
		s := strings.TrimSpace(val)
		if s == "" {
			return 0.0
		}
		s = strings.ReplaceAll(s, ",", ".")
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0.0
		}
		return f
	default:
		s := strings.TrimSpace(fmt.Sprintf("%v", v))
		s = strings.ReplaceAll(s, ",", ".")
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0.0
		}
		return f
	}
}

func (s *Server) handleServiceStatus(w http.ResponseWriter, r *http.Request) {
	mgr := service.NewManager()
	info, err := mgr.Status(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, info)
}

func (s *Server) handleServiceAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Action string `json:"action"`
		Port   int    `json:"port"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Некорректный JSON запрос")
		return
	}

	port := req.Port
	if port <= 0 {
		port = s.cfg.WebPort
	}
	if port <= 0 {
		port = 8080
	}

	mgr := service.NewManager()
	var actionErr error
	switch strings.ToLower(req.Action) {
	case "install", "enable":
		actionErr = mgr.Install(r.Context(), port)
	case "uninstall", "disable":
		actionErr = mgr.Uninstall(r.Context())
	case "start":
		actionErr = mgr.Start(r.Context())
	case "stop":
		actionErr = mgr.Stop(r.Context())
	case "restart":
		actionErr = mgr.Restart(r.Context())
	default:
		respondError(w, http.StatusBadRequest, "Неизвестное действие: "+req.Action)
		return
	}

	if actionErr != nil {
		respondError(w, http.StatusInternalServerError, actionErr.Error())
		return
	}

	info, _ := mgr.Status(r.Context())
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"ok":     true,
		"status": info,
	})
}

func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func respondError(w http.ResponseWriter, status int, message string) {
	respondJSON(w, status, map[string]interface{}{
		"ok":    false,
		"error": message,
	})
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	}
	if cmd != nil {
		_ = cmd.Start()
	}
}
