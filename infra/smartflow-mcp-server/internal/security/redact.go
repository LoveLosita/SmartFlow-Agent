package security

import (
	"regexp"
	"strings"
)

var (
	singleQuotedString = regexp.MustCompile(`'([^'\\]|\\.)*'`)
	doubleQuotedString = regexp.MustCompile(`"([^"\\]|\\.)*"`)
	numericLiteral     = regexp.MustCompile(`\b\d+\b`)
)

func RedactSQL(sql string) string {
	masked := singleQuotedString.ReplaceAllString(sql, "'***'")
	masked = doubleQuotedString.ReplaceAllString(masked, `"***"`)
	masked = numericLiteral.ReplaceAllString(masked, "?")
	return strings.TrimSpace(masked)
}

func RedactKey(key string) string {
	key = strings.TrimSpace(key)
	if len(key) <= 4 {
		return "****"
	}
	return key[:2] + "***" + key[len(key)-2:]
}
