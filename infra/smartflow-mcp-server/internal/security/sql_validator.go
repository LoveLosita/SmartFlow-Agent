package security

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	commentPattern = regexp.MustCompile(`(?s)/\*.*?\*/|--|#`)
	forbiddenWords = regexp.MustCompile(`(?i)\b(INSERT|UPDATE|DELETE|ALTER|DROP|TRUNCATE|CREATE|REPLACE|RENAME|GRANT|REVOKE|MERGE|CALL|EXEC|LOCK|UNLOCK|LOAD|OUTFILE|INFILE|HANDLER|SET|USE)\b`)
	fromJoinRef    = regexp.MustCompile(`(?i)\b(?:FROM|JOIN)\s+([` + "`" + `"\w\.]+)`)
	describeRef    = regexp.MustCompile(`(?i)\b(?:DESCRIBE|DESC)\s+([` + "`" + `"\w\.]+)`)
	showFromRef    = regexp.MustCompile(`(?i)\bSHOW\b[\w\s]*\b(?:FROM|IN)\s+([` + "`" + `"\w\.]+)`)
)

type SQLValidator struct {
	defaultDatabase  string
	enforceWhitelist bool
	allowedDatabases map[string]struct{}
	allowedTables    map[string]struct{}
	allowedTableOnly map[string]struct{}
}

type sqlObjectRef struct {
	database string
	table    string
}

func NewSQLValidator(defaultDatabase string, enforceWhitelist bool, allowedDatabases []string, allowedTables []string) *SQLValidator {
	v := &SQLValidator{
		defaultDatabase:  strings.ToLower(strings.TrimSpace(defaultDatabase)),
		enforceWhitelist: enforceWhitelist,
		allowedDatabases: make(map[string]struct{}),
		allowedTables:    make(map[string]struct{}),
		allowedTableOnly: make(map[string]struct{}),
	}
	for _, db := range allowedDatabases {
		db = strings.ToLower(strings.TrimSpace(db))
		if db != "" {
			v.allowedDatabases[db] = struct{}{}
		}
	}
	for _, tbl := range allowedTables {
		tbl = strings.ToLower(strings.TrimSpace(strings.Trim(tbl, "`\"")))
		if tbl == "" {
			continue
		}
		v.allowedTables[tbl] = struct{}{}
		if dot := strings.LastIndex(tbl, "."); dot >= 0 && dot < len(tbl)-1 {
			v.allowedTableOnly[tbl[dot+1:]] = struct{}{}
		} else {
			v.allowedTableOnly[tbl] = struct{}{}
		}
	}
	return v
}

func (v *SQLValidator) ValidateReadOnlySQL(sql string) error {
	trimmed := strings.TrimSpace(sql)
	if trimmed == "" {
		return fmt.Errorf("sql is required")
	}
	if strings.Contains(trimmed, ";") {
		return fmt.Errorf("semicolon is not allowed")
	}
	if commentPattern.MatchString(trimmed) {
		return fmt.Errorf("sql comments are not allowed")
	}

	first := strings.ToUpper(firstToken(trimmed))
	switch first {
	case "SELECT", "SHOW", "DESCRIBE", "DESC", "EXPLAIN":
	default:
		return fmt.Errorf("only SELECT/SHOW/DESCRIBE/EXPLAIN are allowed")
	}

	withoutLiterals := removeStringLiterals(trimmed)
	if forbiddenWords.MatchString(strings.ToUpper(withoutLiterals)) {
		return fmt.Errorf("dangerous sql keyword detected")
	}

	refs := extractObjectRefs(withoutLiterals)
	if err := v.validateWhitelist(refs); err != nil {
		return err
	}
	return nil
}

func (v *SQLValidator) validateWhitelist(refs []sqlObjectRef) error {
	hasDBAllowlist := len(v.allowedDatabases) > 0
	hasTableAllowlist := len(v.allowedTables) > 0

	if !v.enforceWhitelist && !hasDBAllowlist && !hasTableAllowlist {
		return nil
	}
	if len(refs) == 0 {
		if v.enforceWhitelist {
			return fmt.Errorf("sql does not contain explicit table reference under whitelist mode")
		}
		return nil
	}

	for _, ref := range refs {
		db := ref.database
		tbl := ref.table
		if db == "" {
			db = v.defaultDatabase
		}

		if hasDBAllowlist {
			if db == "" {
				return fmt.Errorf("database is not explicit and no default database set")
			}
			if _, ok := v.allowedDatabases[db]; !ok {
				return fmt.Errorf("database %s is not in whitelist", db)
			}
		}

		if hasTableAllowlist {
			full := tbl
			if db != "" {
				full = db + "." + tbl
			}
			if _, ok := v.allowedTables[full]; ok {
				continue
			}
			if _, ok := v.allowedTableOnly[tbl]; ok {
				continue
			}
			return fmt.Errorf("table %s is not in whitelist", full)
		}
	}

	return nil
}

func extractObjectRefs(sql string) []sqlObjectRef {
	refs := make([]sqlObjectRef, 0)
	seen := make(map[string]struct{})

	for _, re := range []*regexp.Regexp{fromJoinRef, describeRef, showFromRef} {
		matches := re.FindAllStringSubmatch(sql, -1)
		for _, m := range matches {
			if len(m) < 2 {
				continue
			}
			ref := normalizeRef(m[1])
			if ref.table == "" {
				continue
			}
			key := ref.database + "." + ref.table
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			refs = append(refs, ref)
		}
	}
	return refs
}

func normalizeRef(raw string) sqlObjectRef {
	clean := strings.ToLower(strings.TrimSpace(raw))
	clean = strings.Trim(clean, "`\"'(),")
	clean = strings.TrimSpace(clean)
	if clean == "" {
		return sqlObjectRef{}
	}
	parts := strings.Split(clean, ".")
	if len(parts) == 1 {
		return sqlObjectRef{table: parts[0]}
	}
	return sqlObjectRef{database: parts[0], table: parts[len(parts)-1]}
}

func firstToken(sql string) string {
	for i, r := range sql {
		if r == ' ' || r == '\n' || r == '\t' || r == '\r' {
			if i == 0 {
				continue
			}
			return sql[:i]
		}
	}
	return sql
}

func removeStringLiterals(sql string) string {
	masked := singleQuotedString.ReplaceAllString(sql, "''")
	masked = doubleQuotedString.ReplaceAllString(masked, `""`)
	return masked
}
