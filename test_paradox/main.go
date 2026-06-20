package main

import (
	"fmt"
	"httpserverdb/paradox"
	"strings"
)

func main() {
	table, err := paradox.ReadTable("./tables/API.DB")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("API.DB: %d records\n\n", len(table.Records))

	for i, rec := range table.Records {
		cmd, _ := rec["Command"].(string)
		sql := fmt.Sprintf("%v", rec["SQL"])

		if strings.Contains(cmd, "GETSETTING") || strings.Contains(cmd, "AUTH") || i < 3 || i == 16 || i == 31 || i == 43 {
			fmt.Printf("Record %2d: %-45s SQL(%d)=%q\n", i, cmd, len(sql), sql)
		}
	}

	// Verify Setting.DB still works
	fmt.Println("\n=== Setting.DB ===")
	st, _ := paradox.ReadTable("./tables/Setting.DB")
	if len(st.Records) > 0 {
		r := st.Records[0]
		fmt.Printf("Kode=%v, latitude=%v, longitude=%v, MinimumVersion=%v\n",
			r["Kode"], r["Latitude"], r["Longitude"], r["MinimumVersion"])
	}
}
