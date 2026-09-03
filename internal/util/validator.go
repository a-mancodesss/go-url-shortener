package util

import (
	"net/url"
	"regexp"
)

var customAliasRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]{3,20}$`)

// IsValidURL checks if string is a valid http/https URL.
func IsValidURL(s string) bool {
	u, err := url.ParseRequestURI(s)
	if err != nil {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

// IsValidAlias checks custom alias rules.
func IsValidAlias(s string) bool {
	return customAliasRegex.MatchString(s)
}
