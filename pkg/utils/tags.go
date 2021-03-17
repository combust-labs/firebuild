package utils

import "regexp"

const regexpString = "([a-z0-9\\-]{1,60})/([a-z0-9\\-]{1,60}):([a-z0-9.]{1,15})"

// IsValidTag checks if the given image tag is valid.
func IsValidTag(input string) bool {
	re := regexp.MustCompile(regexpString)
	return re.Match([]byte(input))
}

// TagDecompose decomposes the tag into the image components.
func TagDecompose(input string) (bool, string, string, string) {
	re := regexp.MustCompile(regexpString)
	parts := re.FindSubmatch([]byte(input))
	if len(parts) == 4 { // must be 4:
		return true, string(parts[1]), string(parts[2]), string(parts[3])
	}
	return false, "", "", ""
}
