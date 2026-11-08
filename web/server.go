package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
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
	"mycalendar/storage"
)

// Server coordinates the web dashboard, providing a REST API and static frontend.
type Server struct {
	svc          *calendar.Service
	cfg          *models.Config
	cfgPath      string
	version      string
	hostOverride string
	mu           sync.RWMutex
	lastDataMod  time.Time
	lastCfgMod   time.Time
}

// NewServer creates a new web Server instance.
func NewServer(svc *calendar.Service, cfg *models.Config, cfgPath string, version string) *Server {
	if version == "" {
		version = "dev"
	}
	var dataMod, cfgMod time.Time
	if svc != nil {
		dataMod = svc.DataFilesModTime()
	}
	if cfgPath != "" {
		if fi, err := os.Stat(cfgPath); err == nil {
			cfgMod = fi.ModTime()
		}
	}
	return &Server{
		svc:         svc,
		cfg:         cfg,
		cfgPath:     cfgPath,
		version:     version,
		lastDataMod: dataMod,
		lastCfgMod:  cfgMod,
	}
}

// SetHost overrides the bind address for this server instance (e.g. "0.0.0.0" for LAN).
func (s *Server) SetHost(host string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hostOverride = strings.TrimSpace(host)
}

func (s *Server) effectiveHost() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.hostOverride != "" {
		return s.hostOverride
	}
	if s.cfg != nil && s.cfg.WebLAN {
		return "0.0.0.0"
	}
	return "127.0.0.1"
}

// LocalLANIPs returns all active non-loopback IPv4 addresses of the machine.
func LocalLANIPs() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var ips []string
	seen := make(map[string]bool)
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			ip4 := ip.To4()
			if ip4 == nil || ip4.IsLinkLocalUnicast() {
				continue
			}
			s := ip4.String()
			if !seen[s] {
				seen[s] = true
				ips = append(ips, s)
			}
		}
	}
	return ips
}

func (s *Server) checkAutoReload(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 1. Check data file modification
	if s.svc != nil {
		dataMod := s.svc.DataFilesModTime()
		if !dataMod.IsZero() && dataMod.After(s.lastDataMod) {
			if err := s.svc.Reload(ctx); err == nil {
				s.lastDataMod = dataMod
			}
		}
	}

	// 2. Check config file modification
	if s.cfgPath != "" {
		if fi, err := os.Stat(s.cfgPath); err == nil {
			if fi.ModTime().After(s.lastCfgMod) {
				if newCfg, err := config.Load(ctx, s.cfgPath); err == nil {
					s.cfg = newCfg
					s.lastCfgMod = fi.ModTime()
					if s.svc != nil {
						newDir := config.EffectiveDataDir(s.cfg.DataPath, s.cfgPath)
						if newDir != s.svc.DataDir() {
							if newSvc, err := calendar.NewService(ctx, newDir, s.cfg.ActiveEffectiveBaseName(), s.cfg.ActiveSplitMode()); err == nil {
								s.svc = newSvc
								s.lastDataMod = s.svc.DataFilesModTime()
							}
						} else if s.svc.BaseName() != s.cfg.ActiveEffectiveBaseName() || s.svc.Mode() != s.cfg.ActiveSplitMode() {
							_ = s.svc.SwitchCalendar(ctx, s.cfg, s.cfg.ActiveCalendar)
							s.lastDataMod = s.svc.DataFilesModTime()
						}
					}
				}
			}
		}
	}
}

func (s *Server) saveService(ctx context.Context) error {
	err := s.svc.Save(ctx)
	if err == nil {
		s.lastDataMod = s.svc.DataFilesModTime()
	}
	return err
}

// Start launches the HTTP server, listening on 127.0.0.1:port or 0.0.0.0:port (when LAN access is enabled).
func (s *Server) Start(ctx context.Context, defaultPort int, shouldOpenBrowser bool) error {
	host := s.effectiveHost()
	listener, port, err := s.listenOnAvailablePort(host, defaultPort)
	if err != nil {
		return fmt.Errorf("не удалось запустить сервер: %w", err)
	}

	localHost := host
	if localHost == "0.0.0.0" || localHost == "::" || localHost == "" {
		localHost = "127.0.0.1"
	}
	url := fmt.Sprintf("http://%s:%d", localHost, port)
	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════════════════════════════╗")
	fmt.Printf("║  📅  MyCal Web Dashboard запущен: %-34s ║\n", url)
	if host == "0.0.0.0" || host == "::" {
		lanIPs := LocalLANIPs()
		if len(lanIPs) > 0 {
			for _, ip := range lanIPs {
				lanURL := fmt.Sprintf("http://%s:%d", ip, port)
				fmt.Printf("║  🌐  Доступ из сети (LAN):        %-34s ║\n", lanURL)
			}
		} else {
			fmt.Printf("║  🌐  Доступ из сети (LAN):        0.0.0.0:%-26d ║\n", port)
		}
	} else if host != "127.0.0.1" && host != "localhost" {
		fmt.Printf("║  🌐  Доступ из сети (LAN):        %-34s ║\n", url)
	} else {
		fmt.Println("║  💡  Для доступа из сети:         mycal web --lan                    ║")
	}
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

func (s *Server) listenOnAvailablePort(host string, startPort int) (net.Listener, int, error) {
	if strings.TrimSpace(host) == "" {
		host = "127.0.0.1"
	}
	if startPort <= 0 {
		startPort = 8080
	}
	for p := startPort; p < startPort+100; p++ {
		addr := net.JoinHostPort(host, strconv.Itoa(p))
		l, err := net.Listen("tcp", addr)
		if err == nil {
			return l, p, nil
		}
	}
	// Fallback to random system port
	l, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
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
	mux.HandleFunc("/api/sessions/move", s.handleSessionsMove)
	mux.HandleFunc("/api/sessions", s.handleSessions)
	mux.HandleFunc("/api/sessions/", s.handleSessionByID)
	mux.HandleFunc("/api/undo", s.handleUndo)
	mux.HandleFunc("/api/import", s.handleImport)
	mux.HandleFunc("/api/export", s.handleExport)
	mux.HandleFunc("/api/types", s.handleTypes)
	mux.HandleFunc("/api/types/", s.handleTypes)
	mux.HandleFunc("/api/statuses", s.handleStatuses)
	mux.HandleFunc("/api/statuses/", s.handleStatuses)
	mux.HandleFunc("/api/stats", s.handleStats)
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/calendars", s.handleCalendars)
	mux.HandleFunc("/api/calendars/settings", s.handleCalendarSettings)
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
	s.checkAutoReload(r.Context())
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

	now := config.EffectiveToday(s.cfg)
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

	// Sort if requested via query params (?sort=date&order=desc)
	sortField := r.URL.Query().Get("sort")
	if sortField != "" {
		sortOrder := strings.ToLower(r.URL.Query().Get("order"))
		asc := (sortOrder == "asc")
		calendar.SortSessions(sessions, sortField, asc)
	}

	st := calendar.ComputeStats(sessions)
	canUndo, undoDesc := s.svc.CanUndo()
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"sessions":      sessions,
		"count":         st.TotalEntries,
		"total_minutes": st.TotalMinutes,
		"total_hours":   st.TotalHours,
		"total_cash":    st.TotalCash,
		"can_undo":      canUndo,
		"undo_desc":     undoDesc,
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

	s.mu.Lock()
	defer s.mu.Unlock()

	dur := req.Duration
	if dur <= 0 {
		if s.cfg != nil {
			dur = s.cfg.DefaultDuration
		}
		if dur <= 0 {
			dur = 60
		}
	}

	var repeatMode string
	var repeatUntil time.Time
	if req.Repeat != "" && req.Repeat != "none" {
		repeatMode = req.Repeat
		until, errUntil := s.parseDate(req.RepeatUntil)
		if errUntil != nil || until.Before(date) {
			if repeatMode == "m" {
				until = calendar.AddMonths(date, 12)
			} else {
				until = calendar.AddMonths(date, 1)
			}
		}
		repeatUntil = until
	}

	res, err := s.svc.CreateEntries(r.Context(), s.cfg, calendar.EntryInput{
		Date:        date,
		Time:        tm,
		RawName:     name,
		Type:        strings.TrimSpace(req.Type),
		Duration:    dur,
		Cash:        parseCash(req.Cash),
		Status:      strings.TrimSpace(req.Status),
		Notes:       strings.TrimSpace(req.Notes),
		RepeatMode:  repeatMode,
		RepeatUntil: repeatUntil,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Ошибка сохранения записей: "+err.Error())
		return
	}
	s.lastDataMod = s.svc.DataFilesModTime()
	if s.cfg != nil && s.cfgPath != "" {
		_ = config.Save(r.Context(), s.cfgPath, s.cfg)
		if fi, err := os.Stat(s.cfgPath); err == nil {
			s.lastCfgMod = fi.ModTime()
		}
	}

	var first models.Session
	if len(res.Sessions) > 0 {
		first = res.Sessions[0]
	}

	respondJSON(w, http.StatusCreated, map[string]interface{}{
		"ok":        true,
		"count":     res.AddedCount,
		"first_id":  first.ID,
		"series_id": res.SeriesID,
		"session":   first,
	})
}

func (s *Server) handleSessionByID(w http.ResponseWriter, r *http.Request) {
	s.checkAutoReload(r.Context())
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
	SeriesScope  string      `json:"series_scope"`
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

	scope := strings.ToLower(strings.TrimSpace(req.SeriesScope))
	if scope == "" && req.UpdateSeries {
		scope = calendar.SeriesScopeAll
	}
	if scope == "series" {
		scope = calendar.SeriesScopeAll
	}

	var newDatePtr *time.Time
	if req.Date != "" && req.Date != sess.Date() && scope != calendar.SeriesScopeAll && scope != calendar.SeriesScopeFollowing {
		if newDate, err := parseDateLenient(req.Date); err == nil {
			newDatePtr = &newDate
		}
	}

	updated, n, err := s.svc.UpdateEntry(r.Context(), s.cfg, id, calendar.UpdateInput{
		Patch:        patch,
		NewDate:      newDatePtr,
		SeriesScope:  scope,
		UpdateSeries: scope == calendar.SeriesScopeAll,
	})
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.lastDataMod = s.svc.DataFilesModTime()
	if s.cfg != nil && s.cfgPath != "" {
		_ = config.Save(r.Context(), s.cfgPath, s.cfg)
		if fi, err := os.Stat(s.cfgPath); err == nil {
			s.lastCfgMod = fi.ModTime()
		}
	}

	if (scope == calendar.SeriesScopeAll || scope == calendar.SeriesScopeFollowing) && sess.IsRepeat() {
		respondJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "updated_count": n, "session": updated, "scope": scope})
		return
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "session": updated, "scope": "single"})
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

	status := calendar.NormalizeStatus(req.Status)
	if s.cfg != nil && status != "" {
		s.cfg.AddCustomStatus(status)
		if s.cfgPath != "" {
			_ = config.Save(r.Context(), s.cfgPath, s.cfg)
			if fi, err := os.Stat(s.cfgPath); err == nil {
				s.lastCfgMod = fi.ModTime()
			}
		}
	}
	patch := calendar.Patch{Status: &status}
	if err := s.svc.Update(id, patch); err != nil {
		respondError(w, http.StatusNotFound, "Запись не найдена")
		return
	}

	if err := s.saveService(r.Context()); err != nil {
		respondError(w, http.StatusInternalServerError, "Ошибка сохранения: "+err.Error())
		return
	}
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

	cfgChanged := false
	var patch calendar.Patch
	switch strings.ToLower(req.Field) {
	case "name":
		v := strings.TrimSpace(fmt.Sprintf("%v", req.Value))
		if v == "" {
			respondError(w, http.StatusBadRequest, "Наименование не может быть пустым")
			return
		}
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
		if s.cfg != nil && v != "" {
			s.cfg.AddCustomType(v)
			cfgChanged = true
		}
	case "notes":
		v := fmt.Sprintf("%v", req.Value)
		patch.Notes = &v
	case "status":
		v := calendar.NormalizeStatus(fmt.Sprintf("%v", req.Value))
		patch.Status = &v
		if s.cfg != nil && v != "" {
			s.cfg.AddCustomStatus(v)
			cfgChanged = true
		}
	case "cash":
		c := parseCash(req.Value)
		patch.Cash = &c
	case "duration":
		var dur int
		if d, ok := req.Value.(float64); ok {
			dur = int(d)
		} else if sVal, ok := req.Value.(string); ok {
			if parsed, err := strconv.Atoi(strings.TrimSpace(sVal)); err == nil {
				dur = parsed
			}
		}
		if dur <= 0 {
			respondError(w, http.StatusBadRequest, "Длительность должна быть больше 0")
			return
		}
		patch.Duration = &dur
	default:
		respondError(w, http.StatusBadRequest, "Неизвестное поле: "+req.Field)
		return
	}

	if err := s.svc.Update(id, patch); err != nil {
		respondError(w, http.StatusNotFound, "Запись не найдена")
		return
	}

	if cfgChanged && s.cfgPath != "" {
		_ = config.Save(r.Context(), s.cfgPath, s.cfg)
		if fi, err := os.Stat(s.cfgPath); err == nil {
			s.lastCfgMod = fi.ModTime()
		}
	}
	if err := s.saveService(r.Context()); err != nil {
		respondError(w, http.StatusInternalServerError, "Ошибка сохранения: "+err.Error())
		return
	}
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

	scope := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("scope")))
	deleteSeries := r.URL.Query().Get("series") == "true"
	if scope == "all" || scope == "series" || (scope == "" && deleteSeries) {
		if sess.IsRepeat() {
			count := s.svc.DeleteSeries(sess.SeriesID)
			if err := s.saveService(r.Context()); err != nil {
				respondError(w, http.StatusInternalServerError, "Ошибка сохранения: "+err.Error())
				return
			}
			respondJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "deleted": count, "series": true, "scope": "all"})
			return
		}
	} else if scope == "following" {
		if sess.IsRepeat() {
			count := s.svc.DeleteSeriesFrom(sess.SeriesID, id)
			if err := s.saveService(r.Context()); err != nil {
				respondError(w, http.StatusInternalServerError, "Ошибка сохранения: "+err.Error())
				return
			}
			respondJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "deleted": count, "series": true, "scope": "following"})
			return
		}
	}

	if !s.svc.Delete(id) {
		respondError(w, http.StatusNotFound, "Запись не найдена")
		return
	}

	if err := s.saveService(r.Context()); err != nil {
		respondError(w, http.StatusInternalServerError, "Ошибка сохранения: "+err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "deleted": 1, "scope": "single"})
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
		if parsed, errD := s.parseDate(req.Date); errD == nil {
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

	if err := s.saveService(r.Context()); err != nil {
		respondError(w, http.StatusInternalServerError, "Ошибка сохранения: "+err.Error())
		return
	}
	respondJSON(w, http.StatusCreated, map[string]interface{}{"ok": true, "session": dup})
}

func (s *Server) handleTypes(w http.ResponseWriter, r *http.Request) {
	s.checkAutoReload(r.Context())

	switch r.Method {
	case http.MethodGet:
		s.mu.RLock()
		var customTypes, excludedTypes []string
		if s.cfg != nil {
			customTypes = s.cfg.CustomTypes
			excludedTypes = s.cfg.ExcludedTypes
		}
		types := s.svc.AvailableTypes(customTypes, excludedTypes)
		s.mu.RUnlock()
		respondJSON(w, http.StatusOK, map[string]interface{}{"types": types})

	case http.MethodPost:
		var req struct {
			Type  string   `json:"type"`
			Name  string   `json:"name"`
			Types []string `json:"types"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Некорректный JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		var toAdd []string
		if req.Type != "" {
			toAdd = append(toAdd, req.Type)
		}
		if req.Name != "" {
			toAdd = append(toAdd, req.Name)
		}
		toAdd = append(toAdd, req.Types...)

		s.mu.Lock()
		if s.cfg != nil {
			for _, t := range toAdd {
				s.cfg.AddCustomType(t)
			}
			if s.cfgPath != "" {
				_ = config.Save(r.Context(), s.cfgPath, s.cfg)
				if fi, err := os.Stat(s.cfgPath); err == nil {
					s.lastCfgMod = fi.ModTime()
				}
			}
		}
		var customTypes, excludedTypes []string
		if s.cfg != nil {
			customTypes = s.cfg.CustomTypes
			excludedTypes = s.cfg.ExcludedTypes
		}
		types := s.svc.AvailableTypes(customTypes, excludedTypes)
		s.mu.Unlock()

		respondJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "types": types})

	case http.MethodDelete:
		target := strings.TrimPrefix(r.URL.Path, "/api/types/")
		target = strings.TrimPrefix(target, "/")
		if unescaped, err := url.PathUnescape(target); err == nil && unescaped != "" {
			target = unescaped
		}
		if target == "" {
			target = r.URL.Query().Get("name")
		}
		if target == "" {
			target = r.URL.Query().Get("type")
		}
		if target == "" && r.Body != nil {
			var bodyReq struct {
				Name string `json:"name"`
				Type string `json:"type"`
			}
			_ = json.NewDecoder(r.Body).Decode(&bodyReq)
			if bodyReq.Name != "" {
				target = bodyReq.Name
			} else {
				target = bodyReq.Type
			}
		}
		target = strings.TrimSpace(target)
		if target == "" {
			http.Error(w, "Не указан тип для удаления", http.StatusBadRequest)
			return
		}

		s.mu.Lock()
		if s.cfg != nil {
			s.cfg.RemoveType(target)
			if s.cfgPath != "" {
				_ = config.Save(r.Context(), s.cfgPath, s.cfg)
				if fi, err := os.Stat(s.cfgPath); err == nil {
					s.lastCfgMod = fi.ModTime()
				}
			}
		}
		var customTypes, excludedTypes []string
		if s.cfg != nil {
			customTypes = s.cfg.CustomTypes
			excludedTypes = s.cfg.ExcludedTypes
		}
		types := s.svc.AvailableTypes(customTypes, excludedTypes)
		s.mu.Unlock()

		respondJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "types": types})

	default:
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleStatuses(w http.ResponseWriter, r *http.Request) {
	s.checkAutoReload(r.Context())

	switch r.Method {
	case http.MethodGet:
		s.mu.RLock()
		var customStatuses, excludedStatuses []string
		if s.cfg != nil {
			customStatuses = s.cfg.CustomStatuses
			excludedStatuses = s.cfg.ExcludedStatuses
		}
		statuses := s.svc.AvailableStatuses(customStatuses, excludedStatuses)
		s.mu.RUnlock()
		respondJSON(w, http.StatusOK, map[string]interface{}{"statuses": statuses})

	case http.MethodPost:
		var req struct {
			Status   string   `json:"status"`
			Name     string   `json:"name"`
			Statuses []string `json:"statuses"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Некорректный JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		var toAdd []string
		if req.Status != "" {
			toAdd = append(toAdd, req.Status)
		}
		if req.Name != "" {
			toAdd = append(toAdd, req.Name)
		}
		toAdd = append(toAdd, req.Statuses...)

		s.mu.Lock()
		if s.cfg != nil {
			for _, st := range toAdd {
				s.cfg.AddCustomStatus(st)
			}
			if s.cfgPath != "" {
				_ = config.Save(r.Context(), s.cfgPath, s.cfg)
				if fi, err := os.Stat(s.cfgPath); err == nil {
					s.lastCfgMod = fi.ModTime()
				}
			}
		}
		var customStatuses, excludedStatuses []string
		if s.cfg != nil {
			customStatuses = s.cfg.CustomStatuses
			excludedStatuses = s.cfg.ExcludedStatuses
		}
		statuses := s.svc.AvailableStatuses(customStatuses, excludedStatuses)
		s.mu.Unlock()

		respondJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "statuses": statuses})

	case http.MethodDelete:
		target := strings.TrimPrefix(r.URL.Path, "/api/statuses/")
		target = strings.TrimPrefix(target, "/")
		if unescaped, err := url.PathUnescape(target); err == nil && unescaped != "" {
			target = unescaped
		}
		if target == "" {
			target = r.URL.Query().Get("name")
		}
		if target == "" {
			target = r.URL.Query().Get("status")
		}
		if target == "" && r.Body != nil {
			var bodyReq struct {
				Name   string `json:"name"`
				Status string `json:"status"`
			}
			_ = json.NewDecoder(r.Body).Decode(&bodyReq)
			if bodyReq.Name != "" {
				target = bodyReq.Name
			} else {
				target = bodyReq.Status
			}
		}
		target = strings.TrimSpace(target)
		if target == "" {
			http.Error(w, "Не указан статус для удаления", http.StatusBadRequest)
			return
		}

		s.mu.Lock()
		if s.cfg != nil {
			s.cfg.RemoveStatus(target)
			if s.cfgPath != "" {
				_ = config.Save(r.Context(), s.cfgPath, s.cfg)
				if fi, err := os.Stat(s.cfgPath); err == nil {
					s.lastCfgMod = fi.ModTime()
				}
			}
		}
		var customStatuses, excludedStatuses []string
		if s.cfg != nil {
			customStatuses = s.cfg.CustomStatuses
			excludedStatuses = s.cfg.ExcludedStatuses
		}
		statuses := s.svc.AvailableStatuses(customStatuses, excludedStatuses)
		s.mu.Unlock()

		respondJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "statuses": statuses})

	default:
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	s.checkAutoReload(r.Context())
	s.mu.RLock()
	defer s.mu.RUnlock()

	all := s.svc.All()
	st := calendar.ComputeStats(all)
	totalMin := st.TotalMinutes
	totalCash := st.TotalCash
	byTypeMin := st.TypeMinutes
	byTypeCount := st.TypeCounts

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
	s.checkAutoReload(r.Context())
	s.mu.Lock()
	defer s.mu.Unlock()

	switch r.Method {
	case http.MethodGet:
		var cals []models.CalendarInfo
		if s.svc != nil {
			cals = s.svc.ListCalendars(s.cfg)
		}
		eff := s.cfg
		var calSettings *models.CalendarSettings
		if s.svc != nil {
			eff = s.svc.EffectiveConfig(s.cfg)
			calSettings = s.svc.Settings()
		}
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"config":                 s.cfg,
			"effective_config":       eff,
			"calendar_settings":      calSettings,
			"default_visible_fields": calendar.DefaultVisibleFields,
			"effective_today":        config.EffectiveToday(s.cfg).Format("2006-01-02"),
			"calendars":              cals,
			"active_split_mode":      s.cfg.ActiveSplitMode(),
			"version":                s.version,
			"lan_ips":                LocalLANIPs(),
		})
	case http.MethodPut, http.MethodPost:
		updated := *s.cfg
		if err := json.NewDecoder(r.Body).Decode(&updated); err != nil {
			respondError(w, http.StatusBadRequest, "Некорректный JSON конфигурации")
			return
		}
		updated.Normalize()

		oldDataPath := s.cfg.DataPath
		oldDataFileName := s.cfg.DataFileName
		oldEffBaseName := s.cfg.ActiveEffectiveBaseName()
		newDataPath := strings.TrimSpace(updated.DataPath)
		if newDataPath == "" {
			newDataPath = oldDataPath
		} else {
			newDataPath = storage.NormalizeDataPath(newDataPath)
		}

		oldSplit := s.cfg.ActiveSplitMode()
		if updated.SplitMode != "" {
			updated.SetActiveSplitMode(updated.SplitMode)
		}

		if newDataPath != oldDataPath {
			targetDir := config.EffectiveDataDir(newDataPath, s.cfgPath)
			if err := storage.EnsureWritableDir(targetDir); err != nil {
				respondError(w, http.StatusBadRequest, "Недопустимый каталог для хранения базы: "+err.Error())
				return
			}
			updated.DataPath = newDataPath
			if newSvc, err := calendar.NewService(r.Context(), targetDir, updated.ActiveEffectiveBaseName(), updated.ActiveSplitMode()); err == nil {
				s.svc = newSvc
				s.lastDataMod = time.Time{}
			} else {
				respondError(w, http.StatusInternalServerError, "Не удалось переключить каталог записей: "+err.Error())
				return
			}
		} else {
			updated.DataPath = oldDataPath
		}

		*s.cfg = updated
		newSplit := s.cfg.ActiveSplitMode()
		nameChanged := s.cfg.DataFileName != oldDataFileName
		splitChanged := newSplit != "" && newSplit != oldSplit
		if s.svc != nil && (splitChanged || nameChanged) {
			if splitChanged {
				s.svc.SetMode(newSplit)
			}
			if nameChanged {
				s.svc.SetBaseName(s.cfg.ActiveEffectiveBaseName())
			}
			_ = s.svc.Save(r.Context())
			if nameChanged {
				s.svc.RemoveDataFiles(oldEffBaseName)
				storage.RenameFamilyDataFiles(s.svc.DataDir(), oldDataFileName, s.cfg.DataFileName)
			}
			s.lastDataMod = s.svc.DataFilesModTime()
		}
		if s.cfgPath != "" {
			_ = config.Save(r.Context(), s.cfgPath, s.cfg)
			if fi, err := os.Stat(s.cfgPath); err == nil {
				s.lastCfgMod = fi.ModTime()
			}
		}
		var cals []models.CalendarInfo
		if s.svc != nil {
			cals = s.svc.ListCalendars(s.cfg)
		}
		eff := s.cfg
		var calSettings *models.CalendarSettings
		if s.svc != nil {
			eff = s.svc.EffectiveConfig(s.cfg)
			calSettings = s.svc.Settings()
		}
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"ok":                     true,
			"config":                 s.cfg,
			"effective_config":       eff,
			"calendar_settings":      calSettings,
			"default_visible_fields": calendar.DefaultVisibleFields,
			"effective_today":        config.EffectiveToday(s.cfg).Format("2006-01-02"),
			"calendars":              cals,
			"active_split_mode":      s.cfg.ActiveSplitMode(),
			"version":                s.version,
			"lan_ips":                LocalLANIPs(),
		})
	default:
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleCalendars(w http.ResponseWriter, r *http.Request) {
	s.checkAutoReload(r.Context())
	s.mu.Lock()
	defer s.mu.Unlock()

	switch r.Method {
	case http.MethodGet:
		var cals []models.CalendarInfo
		if s.svc != nil {
			cals = s.svc.ListCalendars(s.cfg)
		}
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"calendars":         cals,
			"active_calendar":   s.cfg.ActiveCalendar,
			"active_split_mode": s.cfg.ActiveSplitMode(),
		})

	case http.MethodPost, http.MethodPut:
		var req struct {
			Action    string `json:"action"` // "select" or "create"
			Key       string `json:"key"`
			SplitMode string `json:"split_mode"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, http.StatusBadRequest, "Некорректный JSON запрос")
			return
		}
		action := strings.ToLower(strings.TrimSpace(req.Action))
		if action == "create" {
			cleanKey := config.SanitizeCalendarKey(req.Key)
			if cleanKey == "" {
				respondError(w, http.StatusBadRequest, "Укажите непустой префикс календаря")
				return
			}
			splitMode := req.SplitMode
			switch splitMode {
			case models.SplitNone, models.SplitYear, models.SplitMonth:
			default:
				splitMode = models.SplitNone
			}
			if err := s.svc.CreateCalendar(r.Context(), s.cfg, cleanKey, splitMode); err != nil {
				respondError(w, http.StatusInternalServerError, "Не удалось создать календарь: "+err.Error())
				return
			}
		} else {
			cleanKey := config.SanitizeCalendarKey(req.Key)
			if err := s.svc.SwitchCalendar(r.Context(), s.cfg, cleanKey); err != nil {
				respondError(w, http.StatusInternalServerError, "Не удалось переключить календарь: "+err.Error())
				return
			}
			if req.SplitMode != "" {
				switch req.SplitMode {
				case models.SplitNone, models.SplitYear, models.SplitMonth:
					s.cfg.SetCalendarSplitMode(cleanKey, req.SplitMode)
					s.svc.SetMode(req.SplitMode)
					if err := s.svc.Save(r.Context()); err != nil {
						respondError(w, http.StatusInternalServerError, "Ошибка сохранения календаря: "+err.Error())
						return
					}
				}
			}
		}
		s.lastDataMod = s.svc.DataFilesModTime()
		if s.cfgPath != "" {
			_ = config.Save(r.Context(), s.cfgPath, s.cfg)
			if fi, err := os.Stat(s.cfgPath); err == nil {
				s.lastCfgMod = fi.ModTime()
			}
		}
		cals := s.svc.ListCalendars(s.cfg)
		eff := s.svc.EffectiveConfig(s.cfg)
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"ok":                true,
			"calendars":         cals,
			"active_calendar":   s.cfg.ActiveCalendar,
			"active_split_mode": s.cfg.ActiveSplitMode(),
			"config":            s.cfg,
			"effective_config":  eff,
			"calendar_settings": s.svc.Settings(),
		})

	default:
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleCalendarSettings(w http.ResponseWriter, r *http.Request) {
	s.checkAutoReload(r.Context())
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.svc == nil {
		respondError(w, http.StatusServiceUnavailable, "Сервис календаря недоступен")
		return
	}

	switch r.Method {
	case http.MethodGet:
		eff := s.svc.EffectiveConfig(s.cfg)
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"active_calendar":        s.cfg.ActiveCalendar,
			"active_calendar_name":   s.cfg.ActiveCalendarDisplayName(),
			"settings":               s.svc.Settings(),
			"effective_config":       eff,
			"default_visible_fields": calendar.DefaultVisibleFields,
		})

	case http.MethodPost, http.MethodPut:
		var req struct {
			Reset           bool     `json:"reset"`
			VisibleFields   *string  `json:"visible_fields"`
			DefaultDuration *int     `json:"default_duration"`
			DurationUnit    *string  `json:"duration_unit"`
			DefaultType     *string  `json:"default_type"`
			Theme           *string  `json:"theme"`
			SortField       *string  `json:"sort_field"`
			SortAsc         *bool    `json:"sort_asc"`
			CustomStatuses  []string `json:"custom_statuses"`
			CustomTypes     []string `json:"custom_types"`
			AskOnConflict   *bool    `json:"ask_on_conflict"`
			SilentAddNames  *bool    `json:"silent_add_names"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, http.StatusBadRequest, "Некорректный JSON запрос")
			return
		}

		if req.Reset {
			s.svc.SetSettings(nil)
			if err := s.svc.Save(r.Context()); err != nil {
				respondError(w, http.StatusInternalServerError, "Ошибка сброса настроек: "+err.Error())
				return
			}
			s.lastDataMod = s.svc.DataFilesModTime()
			eff := s.svc.EffectiveConfig(s.cfg)
			respondJSON(w, http.StatusOK, map[string]interface{}{
				"ok":               true,
				"settings":         nil,
				"effective_config": eff,
			})
			return
		}

		settings := s.svc.Settings()
		if settings == nil {
			settings = &models.CalendarSettings{}
		}

		if req.VisibleFields != nil {
			raw := strings.TrimSpace(*req.VisibleFields)
			if raw == "" {
				settings.VisibleFields = nil
			} else {
				norm := calendar.NormalizeVisibleFields(raw)
				settings.VisibleFields = &norm
			}
		}
		if req.DefaultDuration != nil {
			if *req.DefaultDuration <= 0 {
				settings.DefaultDuration = nil
			} else {
				settings.DefaultDuration = req.DefaultDuration
			}
		}
		if req.DurationUnit != nil {
			raw := strings.TrimSpace(*req.DurationUnit)
			if raw == "" {
				settings.DurationUnit = nil
			} else {
				settings.DurationUnit = &raw
			}
		}
		if req.DefaultType != nil {
			raw := strings.TrimSpace(*req.DefaultType)
			if raw == "" {
				settings.DefaultType = nil
			} else {
				settings.DefaultType = &raw
			}
		}
		if req.Theme != nil {
			raw := strings.TrimSpace(strings.ToLower(*req.Theme))
			if raw == "" {
				settings.Theme = nil
			} else {
				settings.Theme = &raw
			}
		}
		if req.SortField != nil {
			raw := strings.TrimSpace(*req.SortField)
			if raw == "" {
				settings.SortField = nil
			} else {
				settings.SortField = &raw
			}
		}
		if req.SortAsc != nil {
			settings.SortAsc = req.SortAsc
		}
		if req.AskOnConflict != nil {
			settings.AskOnConflict = req.AskOnConflict
		}
		if req.SilentAddNames != nil {
			settings.SilentAddNames = req.SilentAddNames
		}
		if len(req.CustomStatuses) > 0 {
			settings.CustomStatuses = req.CustomStatuses
		}
		if len(req.CustomTypes) > 0 {
			settings.CustomTypes = req.CustomTypes
		}

		if settings.IsEmpty() {
			s.svc.SetSettings(nil)
		} else {
			s.svc.SetSettings(settings)
		}

		if err := s.svc.Save(r.Context()); err != nil {
			respondError(w, http.StatusInternalServerError, "Ошибка сохранения настроек: "+err.Error())
			return
		}
		s.lastDataMod = s.svc.DataFilesModTime()
		eff := s.svc.EffectiveConfig(s.cfg)
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"ok":               true,
			"settings":         s.svc.Settings(),
			"effective_config": eff,
		})

	default:
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleSessionsBatch(w http.ResponseWriter, r *http.Request) {
	s.checkAutoReload(r.Context())
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
		s.svc.BeginUndoTx("групповое удаление записей")
		deleted := 0
		for _, id := range req.IDs {
			if s.svc.Delete(id) {
				deleted++
			}
		}
		s.svc.EndUndoTx()
		_ = s.saveService(context.Background())
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
			s.svc.BeginUndoTx("групповой перенос даты")
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
			s.svc.EndUndoTx()
			_ = s.saveService(context.Background())
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
			if s.cfg != nil && v != "" {
				s.cfg.AddCustomType(v)
				if s.cfgPath != "" {
					_ = config.Save(r.Context(), s.cfgPath, s.cfg)
					if fi, err := os.Stat(s.cfgPath); err == nil {
						s.lastCfgMod = fi.ModTime()
					}
				}
			}
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
			v := calendar.NormalizeStatus(fmt.Sprintf("%v", req.Value))
			patch.Status = &v
			if s.cfg != nil && v != "" {
				s.cfg.AddCustomStatus(v)
				if s.cfgPath != "" {
					_ = config.Save(r.Context(), s.cfgPath, s.cfg)
					if fi, err := os.Stat(s.cfgPath); err == nil {
						s.lastCfgMod = fi.ModTime()
					}
				}
			}
		case "cash":
			c := parseCash(req.Value)
			patch.Cash = &c
		default:
			respondError(w, http.StatusBadRequest, "Неизвестное поле для изменения: "+req.Field)
			return
		}

		s.svc.BeginUndoTx("групповое изменение поля")
		for _, id := range req.IDs {
			if err := s.svc.Update(id, patch); err == nil {
				updated++
			}
		}
		s.svc.EndUndoTx()
		if err := s.saveService(r.Context()); err != nil {
			respondError(w, http.StatusInternalServerError, "Ошибка сохранения: "+err.Error())
			return
		}
		respondJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "updated_count": updated})

	default:
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleSessionsMove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}
	s.checkAutoReload(r.Context())
	var req struct {
		IDs            []string `json:"ids"`
		TargetCalendar string   `json:"target_calendar"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Некорректный JSON запрос")
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	moved, err := s.svc.MoveEntriesToCalendar(r.Context(), s.cfg, req.IDs, req.TargetCalendar)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.lastDataMod = s.svc.DataFilesModTime()
	if s.cfg != nil && s.cfgPath != "" {
		_ = config.Save(r.Context(), s.cfgPath, s.cfg)
		if fi, err := os.Stat(s.cfgPath); err == nil {
			s.lastCfgMod = fi.ModTime()
		}
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"ok":          true,
		"moved_count": moved,
		"target":      s.cfg.EffectiveBaseName(req.TargetCalendar),
	})
}

func (s *Server) handleUndo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	desc, err := s.svc.Undo(r.Context())
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.lastDataMod = s.svc.DataFilesModTime()
	canUndo, nextDesc := s.svc.CanUndo()
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"ok":          true,
		"description": desc,
		"can_undo":    canUndo,
		"undo_desc":   nextDesc,
	})
}

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Format   string `json:"format"`
		Filename string `json:"filename"`
		Content  string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Некорректный JSON запрос")
		return
	}
	content := strings.TrimSpace(req.Content)
	if content == "" {
		respondError(w, http.StatusBadRequest, "Файл для импорта пуст")
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	isJSON := strings.EqualFold(req.Format, "json") ||
		strings.HasSuffix(strings.ToLower(req.Filename), ".json") ||
		strings.HasPrefix(content, "{") ||
		strings.HasPrefix(content, "[")

	var res calendar.ImportResult
	var err error
	if isJSON {
		res, err = s.svc.ImportJSON(r.Context(), s.cfg, strings.NewReader(req.Content))
	} else {
		res, err = s.svc.ImportCSV(r.Context(), s.cfg, strings.NewReader(req.Content))
	}
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.lastDataMod = s.svc.DataFilesModTime()
	if res.ConfigMod && s.cfg != nil && s.cfgPath != "" {
		_ = config.Save(r.Context(), s.cfgPath, s.cfg)
		if fi, err := os.Stat(s.cfgPath); err == nil {
			s.lastCfgMod = fi.ModTime()
		}
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"ok":       true,
		"added":    res.Added,
		"imported": res.Added,
		"skipped":  res.Skipped,
		"total":    res.Total,
	})
}

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	s.checkAutoReload(r.Context())
	s.mu.RLock()
	var sessions []models.Session
	if r.Method == http.MethodPost {
		var req struct {
			IDs []string `json:"ids"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if len(req.IDs) > 0 {
			idSet := make(map[string]bool, len(req.IDs))
			for _, id := range req.IDs {
				idSet[id] = true
			}
			for _, sess := range s.svc.All() {
				if idSet[sess.ID] {
					sessions = append(sessions, sess)
				}
			}
		}
	}
	if len(sessions) == 0 {
		sessions = s.svc.All()
	}
	s.mu.RUnlock()

	if len(sessions) == 0 {
		respondError(w, http.StatusBadRequest, "Нет записей для экспорта")
		return
	}
	path, err := s.svc.ExportCSV(sessions)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Ошибка экспорта CSV: "+err.Error())
		return
	}
	csvBytes, _ := os.ReadFile(path)
	if r.URL.Query().Get("download") == "true" {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(path)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(csvBytes)
		return
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"ok":      true,
		"path":    path,
		"count":   len(sessions),
		"content": string(csvBytes),
	})
}

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"app":           "MyCal",
		"version":       s.version,
		"data_dir":      s.svc.DataDir(),
		"total_records": s.svc.Count(),
		"lan_ips":       LocalLANIPs(),
	})
}

// --------------------------- Helper Functions ---------------------------

func (s *Server) parseDate(raw string) (time.Time, error) {
	ref := time.Now()
	if s != nil && s.cfg != nil {
		ref = config.EffectiveToday(s.cfg)
	}
	return parseDateWithRef(raw, ref)
}

func parseDateLenient(s string) (time.Time, error) {
	return parseDateWithRef(s, time.Now())
}

func parseDateWithRef(s string, ref time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return ref, nil
	}
	// Try standard ISO
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}
	// Try parser.ParseDate
	return parser.ParseDate(s, ref)
}

func parseCash(v interface{}) float64 {
	f, _ := calendar.ParseCash(v)
	return f
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
		s.mu.RLock()
		if s.cfg != nil {
			port = s.cfg.WebPort
		}
		s.mu.RUnlock()
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
