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
		writeJSON(w, http.StatusNotFound, map[string]interface{}{
			"status":     "Error",
			"statusCode": "404",
			"data":       map[string]interface{}{"message": "Endpoint not found"},
		})
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
	// instead of a query (e.g. /API/GETPESANSTATIK). Return it verbatim.
	if trimmed := strings.TrimSpace(ep.SQL); strings.HasPrefix(trimmed, "{") {
		var static map[string]interface{}
		if err := json.Unmarshal([]byte(trimmed), &static); err == nil {
			s.log(fmt.Sprintf("  Static response in %v", time.Since(start)))
			writeJSON(w, http.StatusOK, static)
			return
		}
		s.log("  Static SQL field is not valid JSON, falling through to query execution")
	}

	// Execute SQL query
	result, err := ExecuteQuery(s.config.TablesDir, ep.SQL, params)
	if err != nil {
		s.log(fmt.Sprintf("  Error: %v", err))
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"status":     "Error",
			"statusCode": "500",
			"data":       map[string]interface{}{"message": err.Error()},
		})
		return
	}

	// Format response like hasil.txt
	records := make([]map[string]interface{}, 0, len(result.Records))
	for _, rec := range result.Records {
		row := make(map[string]interface{})
		for k, v := range rec {
			// Use lowercase keys in response
			key := strings.ToLower(k)
			if v == nil {
				row[key] = ""
			} else {
				row[key] = v
			}
		}
		records = append(records, row)
	}

	response := map[string]interface{}{
		"status":     "Success",
		"statusCode": "200",
		"data": map[string]interface{}{
			"result": records,
		},
	}

	elapsed := time.Since(start)
	s.log(fmt.Sprintf("  Response: %d records in %v", len(records), elapsed))

	writeJSON(w, http.StatusOK, response)
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(data); err != nil {
		log.Printf("JSON encode error: %v", err)
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
