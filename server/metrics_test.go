package server

import (
	"net/http"
	"sync"
	"testing"
	"time"
)

func TestMetricsPeakRequestsPerSecond(t *testing.T) {
	// Initialize server on a specific test port
	s := NewServer(Config{
		Port:           "18089",
		IPs:            []string{"127.0.0.1"},
		TablesDir:      "../tables",
		SessionTimeout: 8000,
		MaxConnections: 100,
	}, func(msg string) {})

	// Start the server
	if err := s.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer s.Stop()

	// Wait a moment for server to spin up
	time.Sleep(100 * time.Millisecond)

	// Send 15 concurrent requests to the server to establish a high peak RPS
	var wg sync.WaitGroup
	numRequests := 15
	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := http.Get("http://127.0.0.1:18089/invalid-endpoint-for-testing")
			if err == nil {
				resp.Body.Close()
			}
		}()
	}
	wg.Wait()

	// Check metrics
	metrics := s.Metrics()
	if metrics.OnProses < 5 {
		t.Errorf("Expected peak requests-per-second (OnProses) to be at least 5, but got %d", metrics.OnProses)
	}

	peakBefore := metrics.OnProses

	// Wait 1.5 seconds to cross into a new second boundary
	time.Sleep(1500 * time.Millisecond)

	// Send only 2 requests in the new second
	for i := 0; i < 2; i++ {
		resp, err := http.Get("http://127.0.0.1:18089/invalid-endpoint-for-testing")
		if err == nil {
			resp.Body.Close()
		}
	}

	// Verify that the peak remains at the higher value and does not decrease
	metricsAfter := s.Metrics()
	if metricsAfter.OnProses != peakBefore {
		t.Errorf("Peak requests-per-second was modified or decreased. Got %d, expected %d", metricsAfter.OnProses, peakBefore)
	}
}
