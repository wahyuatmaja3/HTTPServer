package server

import (
	"context"
	"encoding/json"
	"fmt"
	"httpserverdb/paradox"
	"log"
	"net/http"
	"path/filepath"
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
}

// Server is the HTTP server
type Server struct {
	config    Config
	endpoints []APIEndpoint
	server    *http.Server
	mu        sync.Mutex
	logFunc   func(string)
}

// NewServer creates a new server instance
func NewServer(config Config, logFunc func(string)) *Server {
	return &Server{
		config:  config,
		logFunc: logFunc,
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

	s.log(fmt.Sprintf("Loaded API.DB: %d records, %d fields", len(table.Records), len(table.Header.Fields)))
	for _, f := range table.Header.Fields {
		s.log(fmt.Sprintf("  Field: %s (type=%d, size=%d)", f.Name, f.Type, f.Size))
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
			s.log(fmt.Sprintf("  Endpoint: %s", ep.Command))
		}
	}

	s.log(fmt.Sprintf("Discovered %d API endpoints", len(s.endpoints)))
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

	// Default handler for unregistered paths
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		s.log(fmt.Sprintf("[%s] %s %s - 404", time.Now().Format("15:04:05"), r.Method, r.URL.Path))
		writeError(w, http.StatusNotFound, "404", "Endpoint not found")
	})

	// Make routing case-insensitive: lowercase the request path before it
	// reaches the mux, so /API/Foo, /api/foo and /API/FOO all match.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.URL.Path = strings.ToLower(r.URL.Path)
		mux.ServeHTTP(w, r)
	})

	addr := fmt.Sprintf(":%s", s.config.Port)
	s.server = &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  time.Duration(s.config.SessionTimeout) * time.Millisecond,
		WriteTimeout: time.Duration(s.config.SessionTimeout) * time.Millisecond,
	}

	s.log(fmt.Sprintf("Starting server on port %s", s.config.Port))

	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.log(fmt.Sprintf("Server error: %v", err))
		}
	}()

	return nil
}

// Stop stops the HTTP server
func (s *Server) Stop() error {
	if s.server == nil {
		return nil
	}
	s.log("Stopping server...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.server.Shutdown(ctx)
}

func (s *Server) handleEndpoint(w http.ResponseWriter, r *http.Request, ep APIEndpoint) {
	start := time.Now()

	// Collect parameters from query string and form
	params := make(map[string]string)
	r.ParseForm()
	for k, v := range r.URL.Query() {
		if len(v) > 0 {
			params[k] = v[0]
		}
	}
	for k, v := range r.PostForm {
		if len(v) > 0 {
			params[k] = v[0]
		}
	}

	// Also try JSON body
	if r.Header.Get("Content-Type") == "application/json" && r.Body != nil {
		var jsonParams map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&jsonParams); err == nil {
			for k, v := range jsonParams {
				params[k] = fmt.Sprintf("%v", v)
			}
		}
	}

	s.log(fmt.Sprintf("[%s] %s %s params=%v", time.Now().Format("15:04:05"), r.Method, r.URL.Path, params))

	// Static endpoints store a ready-made JSON response in the SQL field
	// instead of a query (e.g. /API/GETPESANSTATIK). Return it verbatim,
	// preserving the author's exact key order and formatting.
	if trimmed := strings.TrimSpace(ep.SQL); strings.HasPrefix(trimmed, "{") {
		if json.Valid([]byte(trimmed)) {
			s.log(fmt.Sprintf("  Static response in %v", time.Since(start)))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(trimmed))
			return
		}
		s.log("  Static SQL field is not valid JSON, falling through to query execution")
	}

	// Execute SQL query
	result, err := ExecuteQuery(s.config.TablesDir, ep.SQL, params)
	if err != nil {
		s.log(fmt.Sprintf("  Error: %v", err))
		writeError(w, http.StatusInternalServerError, "500", err.Error())
		return
	}

	elapsed := time.Since(start)
	s.log(fmt.Sprintf("  Response: %d records in %v", len(result.Records), elapsed))

	writeResult(w, "Success", "200", result.Columns, result.Records)
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
