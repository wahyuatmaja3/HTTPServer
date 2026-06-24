// Headless (no-GUI) entry point for HTTPServerDB.
// Reads HTTPServerDB.ini (same format as the GUI) and runs the server
// until interrupted with Ctrl+C.
package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"httpserverdb/server"
)

type iniConfig struct {
	Port           string
	IPs            []string
	MaxConnections int
	SessionTimeout int
	MaxThreads     int
	ListenQueue    int
}

func loadINI(path string) iniConfig {
	cfg := iniConfig{
		Port:           "8024",
		IPs:            []string{"127.0.0.1"},
		MaxConnections: 100,
		SessionTimeout: 8000,
		MaxThreads:     2000,
		ListenQueue:    0,
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return cfg
	}

	cfg.IPs = nil
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "[") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		switch key {
		case "Port":
			cfg.Port = val
		case "IPs":
			// Count line; actual IPs are the IP1..IPn entries. Consume it
			// explicitly so "IPs" isn't mistaken for an IP entry below.
		case "MaxConnections":
			cfg.MaxConnections, _ = strconv.Atoi(val)
		case "SessionTimeOut":
			cfg.SessionTimeout, _ = strconv.Atoi(val)
		case "MaxThreads":
			cfg.MaxThreads, _ = strconv.Atoi(val)
		case "ListenQueue":
			cfg.ListenQueue, _ = strconv.Atoi(val)
		default:
			if strings.HasPrefix(key, "IP") && len(key) > 2 {
				cfg.IPs = append(cfg.IPs, val)
			}
		}
	}

	if len(cfg.IPs) == 0 {
		cfg.IPs = []string{"127.0.0.1"}
	}
	return cfg
}

func main() {
	ini := loadINI("HTTPServerDB.ini")

	config := server.Config{
		Port:           ini.Port,
		IPs:            ini.IPs,
		MaxConnections: ini.MaxConnections,
		SessionTimeout: ini.SessionTimeout,
		MaxThreads:     ini.MaxThreads,
		ListenQueue:    ini.ListenQueue,
		TablesDir:      server.GetTablesDir(),
	}

	srv := server.NewServer(config, func(msg string) {
		log.Println(msg)
	})

	if err := srv.Start(); err != nil {
		log.Fatalf("Error starting server: %v", err)
	}
	log.Printf("Server started on port %s (Ctrl+C to stop)", ini.Port)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	fmt.Println()
	srv.Stop()
	log.Println("Server stopped")
}
