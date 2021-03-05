package rootfs

import "regexp"

const regexpString = "([a-z0-9]{1,60})/([a-z0-9\\-]{1,60}):([a-z0-9.]{1,15})"

func isTagValid(input string) bool {
	re := regexp.MustCompile(regexpString)
	return re.Match([]byte(input))
}

func tagDecompose(input string) (bool, string, string, string) {
	re := regexp.MustCompile(regexpString)
	parts := re.FindSubmatch([]byte(input))
	if len(parts) == 4 { // must be 4:
		return true, string(parts[1]), string(parts[2]), string(parts[3])
	}
	return false, "", "", ""
}