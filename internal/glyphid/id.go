package glyphid

import (
	"crypto/rand"
	"encoding/hex"
	"regexp"
)

var pattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

func Valid(id string) bool { return pattern.MatchString(id) }
func New() (string, error) {
	var b [16]byte
	_, e := rand.Read(b[:])
	return hex.EncodeToString(b[:]), e
}
