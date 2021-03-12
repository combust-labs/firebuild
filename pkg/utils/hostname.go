package utils

import (
	"regexp"
	"strings"

	"github.com/docker/docker/pkg/namesgenerator"
)

// IsValidHostname validates if a string is a valid host name.
func IsValidHostname(host string) bool {
	host = strings.Trim(host, " ")
	re, _ := regexp.Compile(`^(([a-zA-Z0-9]|[a-zA-Z0-9][a-zA-Z0-9\-]*[a-zA-Z0-9])\.)*([A-Za-z0-9]|[A-Za-z0-9][A-Za-z0-9\-]*[A-Za-z0-9])$`)
	if re.MatchString(host) {
		return true
	}
	return false
}

// RandomHostname returns a new random host name.
func RandomHostname() string {
	return strings.ReplaceAll(namesgenerator.GetRandomName(0), "_", "-")
}
