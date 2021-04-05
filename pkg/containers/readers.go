package containers

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/hashicorp/go-hclog"
)

type dockerOutputExtractor func(string) dockerOutput

type dockerOutput interface {
	Captured() string
}

type dockerOutStatus struct {
	Status string `json:"status"`
}

func (d *dockerOutStatus) Captured() string {
	return d.Status
}

func dockerReaderStatus() dockerOutputExtractor {
	return func(raw string) dockerOutput {
		out := &dockerOutStatus{}
		if err := json.Unmarshal([]byte(raw), out); err != nil {
			return nil
		}
		return out
	}
}

type dockerOutStream struct {
	Stream string `json:"stream"`
}

func (d *dockerOutStream) Captured() string {
	return d.Stream
}

func dockerReaderStream() dockerOutputExtractor {
	return func(raw string) dockerOutput {
		out := &dockerOutStream{}
		if err := json.Unmarshal([]byte(raw), out); err != nil {
			return nil
		}
		return out
	}
}

type dockerErrorLine struct {
	Error       string            `json:"error"`
	ErrorDetail dockerErrorDetail `json:"errorDetail"`
}

type dockerErrorDetail struct {
	Message string `json:"message"`
}

func processDockerOutput(logger hclog.Logger, reader io.ReadCloser, lineReader dockerOutputExtractor) error {
	defer reader.Close()
	// read output:
	scanner := bufio.NewScanner(reader)
	lastLine := ""
	for scanner.Scan() {
		lastLine := scanner.Text()
		printable := lineReader(lastLine)
		if printable == nil {
			logger.Warn("Docker output not a stream line, skipping")
			continue
		}
		logger.Info("docker response", "stream", strings.TrimSpace(printable.Captured()))
	}

	// deal with failed builds:
	errLine := &dockerErrorLine{}
	json.Unmarshal([]byte(lastLine), errLine)
	if errLine.Error != "" {
		logger.Error("Docker output finished with error", "reason", errLine.Error)
		return fmt.Errorf(errLine.Error)
	}

	if scannerErr := scanner.Err(); scannerErr != nil {
		logger.Error("Docker response scanner finished with error", "reason", scannerErr)
		return scannerErr
	}

	return nil
}
