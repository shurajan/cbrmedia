package library

import (
	"encoding/base32"
	"strings"

	"github.com/zeebo/blake3"
)

func simpleID(from string) string {
	name := strings.ToLower(strings.TrimSpace(from))
	hash := blake3.Sum256([]byte(name))
	sum := hash[:8]
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(sum)
}
