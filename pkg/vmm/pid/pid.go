package pid

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/combust-labs/firebuild/pkg/utils"
	"github.com/pkg/errors"
)

// RunningVMMPID represents a running VMM pid information.
type RunningVMMPID struct {
	Pid int `json:"pid"`
}

// Wait waits for the process represented by this PID to exit.
func (p *RunningVMMPID) Wait(ctx context.Context) error {
	chanErr := make(chan error, 1)
	go func() {
		// the process is not something we have started so we can't just wait for it...
		for {
			if ctx.Err() != nil {
				close(chanErr)
				return
			}
			proc, err := os.FindProcess(p.Pid)
			if err != nil {
				chanErr <- errors.Wrap(err, "find process")
				break
			}
			err = proc.Signal(syscall.Signal(0))
			if err == nil {
				time.Sleep(time.Second)
				continue
			}
			if err.Error() == "os: process already finished" {
				chanErr <- nil
				break
			}
			errno, ok := err.(syscall.Errno)
			if !ok {
				chanErr <- err
				break
			}
			switch errno {
			case syscall.ESRCH:
				chanErr <- nil
				break
			}
			time.Sleep(time.Second)
		}
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-chanErr:
		return err
	}
}

// FetchPIDIfExists fetches the PID from a pid file in the required directory, if the file exists.
// Returns a PID pointer, if pid file exists, a boolean indicating if PID file existed and an error,
// if PID lookup went wrong.
func FetchPIDIfExists(cacheDirectory string) (*RunningVMMPID, bool, error) {
	pidFile := filepath.Join(cacheDirectory, "pid")
	if _, err := utils.CheckIfExistsAndIsRegular(pidFile); err != nil {
		if !os.IsNotExist(err) {
			return nil, false, err
		}
		if os.IsNotExist(err) {
			return nil, false, nil
		}
	}
	pidJSONBytes, err := ioutil.ReadFile(pidFile)
	if err != nil {
		return nil, false, err
	}
	pidResult := &RunningVMMPID{}
	if jsonErr := json.Unmarshal(pidJSONBytes, pidResult); jsonErr != nil {
		return nil, false, jsonErr
	}
	if pidResult.Pid < 1 {
		return nil, false, fmt.Errorf("invalid pid value in file")
	}
	return pidResult, true, nil
}

// WritePIDToFile writes a PID to a pid file under a directory.
func WritePIDToFile(cacheDirectory string, pid int) error {
	runningMachinePid := &RunningVMMPID{
		Pid: pid,
	}
	pidBytes, jsonErr := json.Marshal(runningMachinePid)
	if jsonErr != nil {
		return errors.Wrap(jsonErr, "failed serializing PID metadata to JSON")
	}
	if err := ioutil.WriteFile(filepath.Join(cacheDirectory, "pid"), []byte(pidBytes), 0644); err != nil {
		return errors.Wrap(err, "failed writing PID metadata the cache file")
	}
	return nil
}
