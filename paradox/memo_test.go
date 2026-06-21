package paradox

import (
	"strings"
	"testing"
)

// TestMemoFullContent verifies that memo (SQL) fields are read completely
// from the .MB file, not truncated to the 50-byte inline portion.
func TestMemoFullContent(t *testing.T) {
	table, err := ReadTable("../tables/API.DB")
	if err != nil {
		t.Fatalf("ReadTable: %v", err)
	}

	var statik string
	got := map[string]string{}
	for _, rec := range table.Records {
		cmd, _ := rec["Command"].(string)
		sql, _ := rec["SQL"].(string)
		got[cmd] = sql
		if cmd == "/API/GETPESANSTATIK" {
			statik = sql
		}
	}

	// The static endpoint must return the full JSON, not a 50-char fragment.
	wantStatik := `{ "status" : "Success", "statusCode" : "200", "data" : { "result" : [] } }`
	if statik != wantStatik {
		t.Errorf("GETPESANSTATIK SQL mismatch\n got: %q (len %d)\nwant: %q (len %d)",
			statik, len(statik), wantStatik, len(wantStatik))
	}

	// A long SELECT must not be truncated at 50 chars and must terminate cleanly.
	long := got["/API/GETABSENUSERTGLRANGE1"]
	if len(long) <= 50 {
		t.Errorf("GETABSENUSERTGLRANGE1 SQL looks truncated: %q (len %d)", long, len(long))
	}
	if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(long)), "SELECT") == false {
		t.Errorf("GETABSENUSERTGLRANGE1 should start with SELECT: %q", long)
	}
}
