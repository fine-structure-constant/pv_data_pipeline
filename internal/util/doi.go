package util

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

var doiPrefixes = []string{
	"https://doi.org/",
	"http://doi.org/",
	"https://dx.doi.org/",
	"http://dx.doi.org/",
	"doi:",
}

func NormalizeDOI(s string) string {
	v := strings.TrimSpace(strings.ToLower(s))
	for _, p := range doiPrefixes {
		if strings.HasPrefix(v, p) {
			v = strings.TrimPrefix(v, p)
			break
		}
	}
	return strings.TrimSpace(v)
}

func TitleHash(title string) string {
	normalized := normalizeTitle(title)
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])
}

func SHA256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

var whitespaceRE = regexp.MustCompile(`\s+`)

func normalizeTitle(title string) string {
	v := strings.ToLower(strings.TrimSpace(title))
	v = whitespaceRE.ReplaceAllString(v, " ")
	return v
}
