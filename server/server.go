package server

import (
	"context"
	"encoding/json"
	"fmt"
	"httpserverdb/paradox"
	"log"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// APIEndpoint represents one API endpoint from the API.DB table
type APIEndpoint struct {
	Nomor   interface{}
	Command string
	SQL     string
	Version interface{}
}

// Config holds the server configuration
type Config struct {
	Port           string
	IPs            []string
	MaxConnections int
	SessionTimeout int
	MaxThreads     int
	ListenQueue    int
	TablesDir      string
	DetailLog      bool
}

// LogColor categorizes a log line so a GUI can render it in a matching color.
// Headless callers (plain logFunc) can ignore it.
type LogColor int

const (
	ColorBlack LogColor = iota
	ColorBlue
	ColorMagenta
	ColorGreen
)

// Metrics is a snapshot of live request counters shown in the GUI.
type Metrics struct {
	TotalReq    int64
	OnProses    int64
	MaxOnProses int64
	MaxTime     time.Duration
}

// Server is the HTTP server
type Server struct {
	config       Config
	endpoints    []APIEndpoint
	server       *http.Server
	mu           sync.Mutex
	logFunc      func(string)
	colorLogFunc func(string, LogColor)

	metricsMu   sync.Mutex
	totalReq    int64
	onProses    int64
	maxOnProses int64
	maxTime     time.Duration

	currentSecond         int64
	currentSecondRequests int64
	peakRequestsPerSecond int64
}

// NewServer creates a new server instance
func NewServer(config Config, logFunc func(string)) *Server {
	return &Server{
		config:  config,
		logFunc: logFunc,
	}
}

// SetColorLog registers a callback that receives per-request log lines with a
// color category. When set, colored lines go here instead of the plain logFunc.
func (s *Server) SetColorLog(fn func(string, LogColor)) {
	s.colorLogFunc = fn
}

// SetDetailLog dynamically enables or disables detailed logging
func (s *Server) SetDetailLog(enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config.DetailLog = enabled
}

// IsDetailLogEnabled returns whether detailed logging is enabled
func (s *Server) IsDetailLogEnabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.config.DetailLog
}

// Metrics returns a snapshot of the live request counters.
func (s *Server) Metrics() Metrics {
	s.metricsMu.Lock()
	defer s.metricsMu.Unlock()
	return Metrics{
		TotalReq:    s.totalReq,
		OnProses:    s.peakRequestsPerSecond,
		MaxOnProses: s.maxOnProses,
		MaxTime:     s.maxTime,
	}
}

// FormatDuration renders a duration as mm:ss:ms (matching the "00:00:140" style used in Finish log lines and the Max Time field).
func FormatDuration(d time.Duration) string {
	totalMs := d.Milliseconds()
	ms := totalMs % 1000
	sec := (totalMs / 1000) % 60
	minu := totalMs / 60000
	return fmt.Sprintf("%02d:%02d:%d", minu, sec, ms)
}

func (s *Server) logColor(msg string, c LogColor) {
	if s.colorLogFunc != nil {
		s.colorLogFunc(msg, c)
	} else if s.logFunc != nil {
		s.logFunc(msg)
	}
}

// LoadEndpoints reads API.DB to discover endpoints
func (s *Server) LoadEndpoints() error {
	apiPath, err := paradox.FindTableFile(s.config.TablesDir, "API")
	if err != nil {
		return fmt.Errorf("find API.DB: %w", err)
	}

	table, err := paradox.ReadTable(apiPath)
	if err != nil {
		return fmt.Errorf("read API.DB: %w", err)
	}

	s.endpoints = nil
	for _, rec := range table.Records {
		ep := APIEndpoint{
			Nomor:   rec["Nomor"],
			Command: fmt.Sprintf("%v", rec["Command"]),
			SQL:     fmt.Sprintf("%v", rec["SQL"]),
			Version: rec["Version"],
		}
		if ep.Command != "" && ep.Command != "<nil>" {
			s.endpoints = append(s.endpoints, ep)
		}
	}

	return nil
}

// Start starts the HTTP server
func (s *Server) Start() error {
	if err := s.LoadEndpoints(); err != nil {
		return err
	}

	mux := http.NewServeMux()

	// Register every endpoint under its lowercase path. Incoming requests are
	// lowercased by the wrapper below, so routing is case-insensitive.
	for _, ep := range s.endpoints {
		ep := ep // capture
		path := ep.Command
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		mux.HandleFunc(strings.ToLower(path), func(w http.ResponseWriter, r *http.Request) {
			s.handleEndpoint(w, r, ep)
		})
	}

	// Default handler for unregistered paths (e.g. /favicon.ico)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "404", "Endpoint not found")
	})

	// Every request is logged Req .. Finish and counted into the metrics, then
	// routed case-insensitively (the path is lowercased before reaching the mux
	// so /API/Foo, /api/foo and /API/FOO all match).
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origPath := r.URL.Path

		s.metricsMu.Lock()
		s.totalReq++
		n := s.totalReq
		s.onProses++
		if s.onProses > s.maxOnProses {
			s.maxOnProses = s.onProses
		}

		// Track peak requests per second
		nowUnix := time.Now().Unix()
		if nowUnix == s.currentSecond {
			s.currentSecondRequests++
		} else {
			if s.currentSecondRequests > s.peakRequestsPerSecond {
				s.peakRequestsPerSecond = s.currentSecondRequests
			}
			s.currentSecond = nowUnix
			s.currentSecondRequests = 1
		}
		if s.currentSecondRequests > s.peakRequestsPerSecond {
			s.peakRequestsPerSecond = s.currentSecondRequests
		}
		s.metricsMu.Unlock()

		if r.URL.RawQuery != "" {
			s.logColor(fmt.Sprintf("%07d Req %s, %s", n, origPath, r.URL.RawQuery), ColorBlue)
		} else {
			s.logColor(fmt.Sprintf("%07d Req %s,", n, origPath), ColorBlue)
		}
		start := time.Now()

		ctx := context.WithValue(r.Context(), "origPath", origPath)
		r = r.WithContext(ctx)

		r.URL.Path = strings.ToLower(r.URL.Path)
		mux.ServeHTTP(w, r)

		dur := time.Since(start)
		s.metricsMu.Lock()
		s.onProses--
		if dur > s.maxTime {
			s.maxTime = dur
		}
		s.metricsMu.Unlock()

		s.logColor(fmt.Sprintf("%07d Finish %s %s ms", n, origPath, FormatDuration(dur)), ColorMagenta)
	})

	addr := fmt.Sprintf(":%s", s.config.Port)
	s.server = &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  time.Duration(s.config.SessionTimeout) * time.Millisecond,
		WriteTimeout: time.Duration(s.config.SessionTimeout) * time.Millisecond,
	}

	// Startup banner (colored for the GUI, matching the original program).
	s.logColor(fmt.Sprintf("MaxConnections  = %d", s.config.MaxConnections), ColorBlue)
	s.logColor(fmt.Sprintf("ListenQueue = %d", s.config.ListenQueue), ColorBlue)
	s.logColor(fmt.Sprintf("SessionTimeOut = %d", s.config.SessionTimeout), ColorBlue)
	s.logColor(fmt.Sprintf("MaxThreads  = %d", s.config.MaxThreads), ColorBlue)
	for _, ip := range s.config.IPs {
		s.logColor(fmt.Sprintf("Server bound to IP %s on port %s", ip, s.config.Port), ColorBlack)
	}

	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logColor(fmt.Sprintf("Server error: %v", err), ColorBlack)
		}
	}()

	s.logColor("Server started", ColorGreen)
	return nil
}

// Stop stops the HTTP server
func (s *Server) Stop() error {
	if s.server == nil {
		return nil
	}
	//s.log("Stopping server...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.server.Shutdown(ctx)
}

func (s *Server) handleEndpoint(w http.ResponseWriter, r *http.Request, ep APIEndpoint) {
	start := time.Now()
	origPath, _ := r.Context().Value("origPath").(string)
	if origPath == "" {
		origPath = r.URL.Path
	}

	// Collect parameters from query string and body only when needed.
	params := make(map[string]string)
	for k, v := range r.URL.Query() {
		if len(v) > 0 {
			params[k] = v[0]
		}
	}

	contentType := r.Header.Get("Content-Type")
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		if strings.HasPrefix(contentType, "application/x-www-form-urlencoded") || strings.HasPrefix(contentType, "multipart/form-data") {
			if err := r.ParseForm(); err == nil {
				for k, v := range r.PostForm {
					if len(v) > 0 {
						params[k] = v[0]
					}
				}
			}
		}
	}

	if strings.HasPrefix(contentType, "application/json") && r.Body != nil {
		var jsonParams map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&jsonParams); err == nil {
			for k, v := range jsonParams {
				params[k] = fmt.Sprintf("%v", v)
			}
		}
	}
	parseDone := time.Now()

	// Static endpoints store a ready-made JSON response in the SQL field
	// instead of a query (e.g. /API/GETPESANSTATIK). Return it verbatim,
	// preserving the author's exact key order and formatting.
	if trimmed := strings.TrimSpace(ep.SQL); strings.HasPrefix(trimmed, "{") {
		if json.Valid([]byte(trimmed)) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(trimmed))
			return
		}
		s.log("  Static SQL field is not valid JSON, falling through to query execution")
	}

	var result *QueryResult
	var err error

	if s.IsDetailLogEnabled() {
		// 1. Log Query1.SQL.Text
		s.logColor(fmt.Sprintf("%s Query1.SQL.Text", origPath), ColorBlack)

		// 2. Extract and log parameters
		placeholderRegex := regexp.MustCompile(`:[a-zA-Z_][a-zA-Z0-9_]*`)
		matches := placeholderRegex.FindAllString(ep.SQL, -1)
		for i := range matches {
			s.logColor(fmt.Sprintf("%s Param%d", origPath, i), ColorBlack)
		}

		// 3. Log Query1.Open if it's a SELECT query
		upperSQL := strings.ToUpper(strings.TrimSpace(ep.SQL))
		isSelect := strings.HasPrefix(upperSQL, "SELECT")
		if isSelect {
			s.logColor(fmt.Sprintf("%s Query1.Open", origPath), ColorBlack)
		}

		// Execute SQL query
		result, err = ExecuteQuery(s.config.TablesDir, ep.SQL, params)

		// 4. Log Result <count> if it's a SELECT query and there was no error
		if isSelect && err == nil {
			s.logColor(fmt.Sprintf("%sResult %d", origPath, len(result.Records)), ColorBlack)
		}
	} else {
		// Execute SQL query normally without detail logs
		result, err = ExecuteQuery(s.config.TablesDir, ep.SQL, params)
	}
	queryDone := time.Now()
	if err != nil {
		s.log(fmt.Sprintf("  Error: %v", err))
		writeError(w, http.StatusInternalServerError, "500", err.Error())
		return
	}

	writeResult(w, "Success", "200", result.Columns, result.Records)
	writeDone := time.Now()
	if total := writeDone.Sub(start); total >= 500*time.Millisecond {
		s.log(fmt.Sprintf("SLOW %s parse=%v query=%v write=%v total=%v rows=%d", ep.Command, parseDone.Sub(start), queryDone.Sub(parseDone), writeDone.Sub(queryDone), total, len(result.Records)))
	}
}

// writeResult emits the response in the exact shape produced by the original
// server (see hasil.txt): fixed top-level key order (status, statusCode, data),
// column order and casing preserved from the SQL, and every value as a string.
func writeResult(w http.ResponseWriter, status, statusCode string, columns []string, records []paradox.Record) {
	var sb strings.Builder
	sb.WriteString(`{ "status" : `)
	sb.Write(jsonString(status))
	sb.WriteString(`, "statusCode" : `)
	sb.Write(jsonString(statusCode))
	sb.WriteString(`, "data" : { "result" : [`)
	for i, rec := range records {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString("{ ")
		for j, key := range columns {
			if j > 0 {
				sb.WriteString(", ")
			}
			sb.Write(jsonString(key))
			sb.WriteString(" : ")
			sb.Write(jsonString(valueToString(rec[key])))
		}
		sb.WriteString(" }")
	}
	sb.WriteString("] } }")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(sb.String()))
}

// writeError emits an error response matching the original server's shape:
// { "status" : "Error", "statusCode" : "<code>", "data" : { "message" : "<msg>" } }
func writeError(w http.ResponseWriter, httpStatus int, statusCode, message string) {
	var sb strings.Builder
	sb.WriteString(`{ "status" : "Error", "statusCode" : `)
	sb.Write(jsonString(statusCode))
	sb.WriteString(`, "data" : { "message" : `)
	sb.Write(jsonString(message))
	sb.WriteString(` } }`)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(httpStatus)
	w.Write([]byte(sb.String()))
}

func jsonString(s string) []byte {
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.Encode(s)
	return []byte(strings.TrimRight(buf.String(), "\n"))
}

func valueToString(v interface{}) string {
	switch x := v.(type) {
	case nil:
		return ""
	case bool:
		if x {
			return "True"
		}
		return "False"
	case string:
		return x
	default:
		return fmt.Sprintf("%v", x)
	}
}

func (s *Server) log(msg string) {
	if s.logFunc != nil {
		s.logFunc(msg)
	}
	log.Println(msg)
}

// GetTablesDir returns the tables directory path relative to the exe
func GetTablesDir() string {
	return filepath.Join(".", "tables")
}
