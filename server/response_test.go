package server

import (
	"net/http/httptest"
	"testing"

	"httpserverdb/paradox"
)

// TestWriteResultFormat locks the success response shape to match the original
// server (see hasil.txt): fixed top-level key order, column order/casing
// preserved, every value rendered as a string.
func TestWriteResultFormat(t *testing.T) {
	rec := httptest.NewRecorder()
	columns := []string{"kode", "lockgps", "MinimumVersion"}
	records := []paradox.Record{
		{"kode": "00001", "lockgps": false, "MinimumVersion": "1.0.19"},
	}
	writeResult(rec, "Success", "200", columns, records)

	want := `{ "status" : "Success", "statusCode" : "200", "data" : { "result" : [{ "kode" : "00001", "lockgps" : "False", "MinimumVersion" : "1.0.19" }] } }`
	if got := rec.Body.String(); got != want {
		t.Errorf("response mismatch\n got: %s\nwant: %s", got, want)
	}
}

func TestWriteResultEmpty(t *testing.T) {
	rec := httptest.NewRecorder()
	writeResult(rec, "Success", "200", []string{"x"}, nil)
	want := `{ "status" : "Success", "statusCode" : "200", "data" : { "result" : [] } }`
	if got := rec.Body.String(); got != want {
		t.Errorf("empty result mismatch\n got: %s\nwant: %s", got, want)
	}
}

func TestWriteErrorFormat(t *testing.T) {
	rec := httptest.NewRecorder()
	writeError(rec, 404, "404", "Endpoint not found")
	want := `{ "status" : "Error", "statusCode" : "404", "data" : { "message" : "Endpoint not found" } }`
	if got := rec.Body.String(); got != want {
		t.Errorf("error mismatch\n got: %s\nwant: %s", got, want)
	}
}
