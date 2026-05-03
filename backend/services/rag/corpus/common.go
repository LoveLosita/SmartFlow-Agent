package corpus

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

func hashLikeText(text string) string {
	normalized := strings.TrimSpace(strings.ToLower(text))
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:8])
}
