package containers

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	fileCommandExtractor = regexp.MustCompile("(ADD|COPY)\\sfile:[a-zA-Z0-9]{64}\\sin\\s")
)

// HistoryToDockerfile reconstructs the Dockerfile from the image history.
func HistoryToDockerfile(history []*DockerImageHistoryEntry, base string) []string {
	lines := []string{fmt.Sprintf("FROM %s", base)}
	for _, entry := range history {
		// split entry line by #(nop)
		parts := strings.Split(entry.CreatedBy, "#(nop)")
		if len(parts) != 2 {
			// skip unexpected lines
			continue
		}
		// the Docker command is trimmed parts[1]
		dockerCommand := strings.TrimSpace(parts[1])
		// we need to take care of ADD and COPY:
		if strings.HasPrefix(dockerCommand, "ADD") || strings.HasPrefix(dockerCommand, "COPY") {

			if len(lines) == 1 && strings.HasPrefix(dockerCommand, "ADD") && strings.HasSuffix(dockerCommand, "in /") {
				// skip the 'ADD file:... in /' which represents adding the rootfs
				continue
			}

			lines = append(lines, reconstructFileCommand(dockerCommand))

		} else {
			lines = append(lines, dockerCommand)
		}
	}
	return lines
}

func reconstructFileCommand(input string) string {
	// CONSIDER: maybe the parsing has to be a bit more bulletproof but for now, it does the job...
	path := fileCommandExtractor.ReplaceAllString(input, "")
	if strings.HasPrefix(input, "ADD") {
		return fmt.Sprintf("ADD %s %s", path, path)
	}
	return fmt.Sprintf("COPY %s %s", path, path)
}
