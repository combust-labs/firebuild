package pid

import (
	"context"
	"fmt"
	"os"
	"syscall"
	"time"
)

// RunningVMMPID represents a running VMM pid information.
type RunningVMMPID struct {
	Pid int `json:"pid"`
}

// IsRunning checks if the process identified by the PID is still running.
func (p *RunningVMMPID) IsRunning() (bool, error) {
	if p.Pid <= 0 {
		return false, fmt.Errorf("invalid pid %v", p.Pid)
	}
	proc, err := os.FindProcess(p.Pid)
	if err != nil {
		return false, err
	}
	err = proc.Signal(syscall.Signal(0))
	if err == nil {
		return true, nil
	}
	if err.Error() == "os: process already finished" {
		return false, nil
	}
	errno, ok := err.(syscall.Errno)
	if !ok {
		return false, err
	}
	switch errno {
	case syscall.ESRCH:
		return false, nil
	case syscall.EPERM:
		return true, nil
	}
	return false, err
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
			isRunning, err := p.IsRunning()
			if err != nil {
				chanErr <- err
				break
			}
			if isRunning {
				continue
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
