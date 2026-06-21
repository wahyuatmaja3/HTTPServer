package server

import (
	"fmt"
	"httpserverdb/paradox"
	"regexp"
	"strings"
)

// QueryResult holds the result of executing a query
type QueryResult struct {
	Records []paradox.Record
	Columns []string
}

// ParsedQuery represents a parsed SQL-like query
type ParsedQuery struct {
	Type      string // SELECT, INSERT, UPDATE
	Columns   []string
	TableName string
	Alias     string
	Where     []WhereClause
	OrderBy   []OrderClause
	GroupBy   []string
	Joins     []JoinInfo
	IsSelectAll bool
}

type WhereClause struct {
	Field    string
	Op       string
	Param    string
	IsLiteral bool
	LiteralVal string
}

type OrderClause struct {
	Field string
	Desc  bool
}

type JoinInfo struct {
	Table string
	Alias string
	OnLeft  string
	OnRight string
}

// ExecuteQuery executes a simple SQL-like query against Paradox tables
func ExecuteQuery(tablesDir string, sql string, params map[string]string) (*QueryResult, error) {
	sql = strings.TrimSpace(sql)
	if sql == "" {
		return &QueryResult{}, nil
	}

	upperSQL := strings.ToUpper(sql)

	if strings.HasPrefix(upperSQL, "SELECT") {
		return executeSelect(tablesDir, sql, params)
	} else if strings.HasPrefix(upperSQL, "INSERT") {
		return executeInsert(tablesDir, sql, params)
	} else if strings.HasPrefix(upperSQL, "UPDATE") {
		return executeUpdate(tablesDir, sql, params)
	}

	return nil, fmt.Errorf("unsupported SQL: %s", sql[:min(50, len(sql))])
}

func executeSelect(tablesDir string, sql string, params map[string]string) (*QueryResult, error) {
	parsed, err := parseSelectSQL(sql)
	if err != nil {
		return nil, err
	}

	// Load the main table
	tablePath, err := paradox.FindTableFile(tablesDir, parsed.TableName)
	if err != nil {
		return nil, fmt.Errorf("table not found: %s: %w", parsed.TableName, err)
	}

	table, err := paradox.ReadTable(tablePath)
	if err != nil {
		return nil, fmt.Errorf("read table %s: %w", parsed.TableName, err)
	}

	// Filter records based on WHERE clauses
	var filtered []paradox.Record
	for _, rec := range table.Records {
		if matchesWhere(rec, parsed.Where, params, parsed.Alias) {
			filtered = append(filtered, rec)
		}
	}

	// Determine the ordered output keys, preserving the exact name/casing
	// each column has in the SQL (or its alias). Response key order follows
	// SQL column order.
	cols := parsed.Columns
	if parsed.IsSelectAll {
		cols = table.GetFieldNames()
	}

	result := &QueryResult{Columns: outputKeys(cols)}

	for _, rec := range filtered {
		projected := make(paradox.Record)
		for _, col := range cols {
			// Handle aliased columns like "k.Kode"
			fieldName := col
			if idx := strings.Index(col, "."); idx >= 0 {
				fieldName = col[idx+1:]
			}
			// Handle column aliases like "LockGPS kLockgps"
			parts := strings.Fields(col)
			if len(parts) >= 2 {
				fieldName = parts[0]
				if idx := strings.Index(fieldName, "."); idx >= 0 {
					fieldName = fieldName[idx+1:]
				}
				alias := parts[len(parts)-1]
				projected[alias] = getFieldValue(rec, fieldName)
			} else {
				projected[fieldName] = getFieldValue(rec, fieldName)
			}
		}

		// Handle :param AS alias expressions
		for _, col := range cols {
			upperCol := strings.ToUpper(col)
			if strings.Contains(upperCol, " AS ") {
				parts := strings.SplitN(col, " as ", 2)
				if len(parts) < 2 {
					parts = strings.SplitN(col, " AS ", 2)
				}
				if len(parts) == 2 {
					expr := strings.TrimSpace(parts[0])
					alias := strings.TrimSpace(parts[1])
					if strings.HasPrefix(expr, ":") {
						paramName := expr[1:]
						if v, ok := params[paramName]; ok {
							projected[alias] = v
						}
					}
				}
			}
		}

		result.Records = append(result.Records, projected)
	}

	return result, nil
}

// outputKeys maps SQL column expressions to the response key for each,
// preserving order and original casing.
func outputKeys(cols []string) []string {
	keys := make([]string, 0, len(cols))
	for _, col := range cols {
		if idx := findKeyword(strings.ToUpper(col), "AS"); idx >= 0 {
			keys = append(keys, strings.TrimSpace(col[idx+2:]))
			continue
		}
		parts := strings.Fields(col)
		key := parts[0]
		if len(parts) >= 2 {
			key = parts[len(parts)-1] // column alias: "LockGPS kLockgps"
		}
		if dot := strings.LastIndex(key, "."); dot >= 0 {
			key = key[dot+1:]
		}
		keys = append(keys, key)
	}
	return keys
}

func executeInsert(tablesDir string, sql string, params map[string]string) (*QueryResult, error) {
	// For INSERT, we just return a success response without actually modifying files
	// Since Paradox tables are read-only in this implementation
	return &QueryResult{
		Records: []paradox.Record{
			{"status": "inserted"},
		},
		Columns: []string{"status"},
	}, nil
}

func executeUpdate(tablesDir string, sql string, params map[string]string) (*QueryResult, error) {
	// For UPDATE, return success without modifying files
	return &QueryResult{
		Records: []paradox.Record{
			{"status": "updated"},
		},
		Columns: []string{"status"},
	}, nil
}

var selectRegex = regexp.MustCompile(`(?is)select\s+(.+?)\s+from\s+('[^']+?'|[^\s,]+)(?:\s+([a-zA-Z]\w*))?\s*(?:,\s*('[^']+?'|[^\s,]+)(?:\s+([a-zA-Z]\w*))?\s*)*(?:\s+where\s+(.+?))?(?:\s+group\s+by\s+(.+?))?(?:\s+order\s+by\s+(.+?))?$`)

func parseSelectSQL(sql string) (*ParsedQuery, error) {
	// Clean up SQL
	sql = strings.ReplaceAll(sql, "\r\n", " ")
	sql = strings.ReplaceAll(sql, "\n", " ")
	sql = strings.TrimSpace(sql)

	parsed := &ParsedQuery{Type: "SELECT"}

	upper := strings.ToUpper(sql)

	// Find SELECT ... FROM
	fromIdx := findKeyword(upper, "FROM")
	if fromIdx < 0 {
		return nil, fmt.Errorf("no FROM clause found")
	}

	selectPart := strings.TrimSpace(sql[7:fromIdx]) // skip "SELECT "
	if selectPart == "*" {
		parsed.IsSelectAll = true
	} else {
		parsed.Columns = parseColumnList(selectPart)
	}

	// Get FROM clause
	remaining := strings.TrimSpace(sql[fromIdx+5:]) // skip "FROM "

	// Find WHERE, GROUP BY, ORDER BY positions
	upperRemaining := strings.ToUpper(remaining)
	whereIdx := findKeyword(upperRemaining, "WHERE")
	groupIdx := findKeyword(upperRemaining, "GROUP BY")
	orderIdx := findKeyword(upperRemaining, "ORDER BY")

	// Extract table name (possibly with alias)
	var fromEnd int
	if whereIdx >= 0 {
		fromEnd = whereIdx
	} else if groupIdx >= 0 {
		fromEnd = groupIdx
	} else if orderIdx >= 0 {
		fromEnd = orderIdx
	} else {
		fromEnd = len(remaining)
	}

	fromClause := strings.TrimSpace(remaining[:fromEnd])
	parseFromClause(fromClause, parsed)

	// Parse WHERE
	if whereIdx >= 0 {
		var whereEnd int
		if groupIdx >= 0 {
			whereEnd = groupIdx
		} else if orderIdx >= 0 {
			whereEnd = orderIdx
		} else {
			whereEnd = len(remaining)
		}
		whereClause := strings.TrimSpace(remaining[whereIdx+6 : whereEnd])
		parsed.Where = parseWhereClause(whereClause)
	}

	return parsed, nil
}

func findKeyword(upper string, keyword string) int {
	// Find keyword not inside quotes
	idx := 0
	for {
		pos := strings.Index(upper[idx:], keyword)
		if pos < 0 {
			return -1
		}
		absPos := idx + pos
		// Check it's a word boundary
		if absPos > 0 {
			prev := upper[absPos-1]
			if prev != ' ' && prev != '\t' && prev != '\n' && prev != '(' {
				idx = absPos + len(keyword)
				continue
			}
		}
		if absPos+len(keyword) < len(upper) {
			next := upper[absPos+len(keyword)]
			if next != ' ' && next != '\t' && next != '\n' {
				idx = absPos + len(keyword)
				continue
			}
		}
		return absPos
	}
}

func parseColumnList(s string) []string {
	var cols []string
	depth := 0
	start := 0
	for i, ch := range s {
		switch ch {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				col := strings.TrimSpace(s[start:i])
				if col != "" {
					cols = append(cols, col)
				}
				start = i + 1
			}
		}
	}
	col := strings.TrimSpace(s[start:])
	if col != "" {
		cols = append(cols, col)
	}
	return cols
}

func parseFromClause(fromClause string, parsed *ParsedQuery) {
	// Handle multiple tables (joins)
	tables := strings.Split(fromClause, ",")
	for i, t := range tables {
		t = strings.TrimSpace(t)
		parts := strings.Fields(t)
		if len(parts) == 0 {
			continue
		}
		tableName := strings.Trim(parts[0], "'\"")
		alias := ""
		if len(parts) > 1 {
			alias = parts[1]
		}

		// Clean table name - extract just the table name from paths
		if strings.Contains(tableName, "\\") || strings.Contains(tableName, "/") {
			// Extract filename without extension
			lastSep := strings.LastIndexAny(tableName, "\\/")
			if lastSep >= 0 {
				tableName = tableName[lastSep+1:]
			}
			if idx := strings.LastIndex(tableName, "."); idx >= 0 {
				tableName = tableName[:idx]
			}
		}

		if i == 0 {
			parsed.TableName = tableName
			parsed.Alias = alias
		}
	}
}

func parseWhereClause(where string) []WhereClause {
	var clauses []WhereClause

	// Split on AND (case insensitive)
	parts := splitOnAnd(where)

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		clause := parseCondition(part)
		if clause != nil {
			clauses = append(clauses, *clause)
		}
	}

	return clauses
}

func splitOnAnd(s string) []string {
	upper := strings.ToUpper(s)
	var parts []string
	start := 0
	depth := 0

	for i := 0; i < len(upper); i++ {
		switch upper[i] {
		case '(':
			depth++
		case ')':
			depth--
		default:
			if depth == 0 && i+5 <= len(upper) && upper[i:i+5] == " AND " {
				parts = append(parts, s[start:i])
				start = i + 5
				i += 4
			}
		}
	}
	parts = append(parts, s[start:])
	return parts
}

func parseCondition(cond string) *WhereClause {
	cond = strings.TrimSpace(cond)

	// Handle LIKE
	if idx := findKeyword(strings.ToUpper(cond), "LIKE"); idx >= 0 {
		field := strings.TrimSpace(cond[:idx])
		param := strings.TrimSpace(cond[idx+5:])
		return &WhereClause{Field: field, Op: "LIKE", Param: cleanParam(param)}
	}

	// Handle BETWEEN
	if idx := findKeyword(strings.ToUpper(cond), "BETWEEN"); idx >= 0 {
		// Skip BETWEEN for now
		return nil
	}

	// Handle = operator
	if idx := strings.Index(cond, "="); idx >= 0 {
		// Make sure it's not != or >=
		if idx > 0 && (cond[idx-1] == '!' || cond[idx-1] == '>' || cond[idx-1] == '<') {
			return nil
		}
		field := strings.TrimSpace(cond[:idx])
		val := strings.TrimSpace(cond[idx+1:])

		clause := &WhereClause{Field: field, Op: "="}

		if strings.HasPrefix(val, ":") {
			clause.Param = val[1:]
		} else {
			clause.IsLiteral = true
			clause.LiteralVal = strings.Trim(val, "'\"")
		}
		return clause
	}

	return nil
}

func cleanParam(p string) string {
	p = strings.TrimSpace(p)
	if strings.HasPrefix(p, ":") {
		p = p[1:]
	}
	return p
}

func matchesWhere(rec paradox.Record, clauses []WhereClause, params map[string]string, tableAlias string) bool {
	for _, clause := range clauses {
		fieldName := clause.Field
		// Remove table alias prefix
		if idx := strings.Index(fieldName, "."); idx >= 0 {
			fieldName = fieldName[idx+1:]
		}

		recVal := getFieldValue(rec, fieldName)
		recStr := fmt.Sprintf("%v", recVal)
		if recVal == nil {
			recStr = ""
		}

		var compareVal string
		if clause.IsLiteral {
			compareVal = clause.LiteralVal
		} else if clause.Param != "" {
			var ok bool
			compareVal, ok = params[clause.Param]
			if !ok {
				// Try case-insensitive param match
				for k, v := range params {
					if strings.EqualFold(k, clause.Param) {
						compareVal = v
						ok = true
						break
					}
				}
				if !ok {
					continue // Skip this clause if param not provided
				}
			}
		}

		switch clause.Op {
		case "=":
			if !strings.EqualFold(recStr, compareVal) {
				return false
			}
		case "LIKE":
			pattern := strings.ReplaceAll(compareVal, "%", ".*")
			matched, _ := regexp.MatchString("(?i)^"+pattern+"$", recStr)
			if !matched {
				return false
			}
		}
	}
	return true
}

func getFieldValue(rec paradox.Record, fieldName string) interface{} {
	// Try exact match
	if v, ok := rec[fieldName]; ok {
		return v
	}
	// Try case-insensitive
	lower := strings.ToLower(fieldName)
	for k, v := range rec {
		if strings.ToLower(k) == lower {
			return v
		}
	}
	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
