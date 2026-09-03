package util

import (
	"strings"
)

// Base62 alphabet — 62 characters [0-9a-zA-Z].
// Chosen to keep short codes URL-safe without encoding.
const alphabet = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

var alphabetMap map[byte]int

func init() {
	alphabetMap = make(map[byte]int, len(alphabet))
	for i := 0; i < len(alphabet); i++ {
		alphabetMap[alphabet[i]] = i
	}
}

// EncodeBase62 encodes a positive int64 to a Base62 string.
// Example: 125 -> "21"
func EncodeBase62(num int64) string {
	if num == 0 {
		return string(alphabet[0])
	}
	var sb strings.Builder
	base := int64(len(alphabet))
	for num > 0 {
		rem := num % base
		sb.WriteByte(alphabet[rem])
		num /= base
	}
	// Reverse string (we built it LSB first)
	s := sb.String()
	runes := []rune(s)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	return string(runes)
}

// DecodeBase62 decodes a Base62 string back to int64.
// Returns -1 and false if invalid character found.
func DecodeBase62(s string) (int64, bool) {
	var num int64
	base := int64(len(alphabet))
	for i := 0; i < len(s); i++ {
		val, ok := alphabetMap[s[i]]
		if !ok {
			return -1, false
		}
		num = num*base + int64(val)
	}
	return num, true
}

// PadLeft pads s to length with alphabet[0] ('0') on the left.
// Useful to enforce minimum short code length (e.g. 7 chars).
func PadLeft(s string, length int) string {
	if len(s) >= length {
		return s
	}
	return strings.Repeat(string(alphabet[0]), length-len(s)) + s
}
