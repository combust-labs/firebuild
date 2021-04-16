package flock

import (
	"syscall"
	"time"
)

type acquireTimeoutError string
type acquireFailedError string

// ErrTimeout indicates that the lock attempt timed out.
var ErrTimeout error = acquireTimeoutError("acquire timeout exceeded")

func (t acquireTimeoutError) Error() string {
	return string(t)
}

// ErrLocked indicates TryLock failed because the lock was already locked.
var ErrLocked error = acquireFailedError("already locked")

func (t acquireFailedError) Error() string {
	return string(t)
}

// Lock implements flock syscall based cross-process locking.
type Lock interface {
	Acquire() error
	AcquireWithTimeout(time.Duration) error
	TryAcquire() error
	Release() error
}

type defaultLock struct {
	filename string
	fd       int
}

// New returns a new lock around the given file.
func New(filename string) Lock {
	return &defaultLock{filename: filename}
}

// Acquire attempts acquiring the lock. Will block until the lock becomes available.
func (l *defaultLock) Acquire() error {
	if err := l.open(); err != nil {
		return err
	}
	return syscall.Flock(l.fd, syscall.LOCK_EX)
}

// AcquireWithTimeout attempts to acquire the lock until the timeout expires. Blocking.
func (l *defaultLock) AcquireWithTimeout(timeout time.Duration) error {
	if err := l.open(); err != nil {
		return err
	}
	result := make(chan error)
	cancel := make(chan struct{})
	go func() {
		err := syscall.Flock(l.fd, syscall.LOCK_EX)
		select {
		case <-cancel: // Timed out, maybe cleanup.
			syscall.Flock(l.fd, syscall.LOCK_UN)
			syscall.Close(l.fd)
		case result <- err:
		}
	}()
	select {
	case err := <-result:
		return err
	case <-time.After(timeout):
		close(cancel)
		return ErrTimeout
	}
}

// Release releases the flock.
func (l *defaultLock) Release() error {
	return syscall.Close(l.fd)
}

// TryLock attempts to lock the lock.  This method will return ErrLocked
// immediately if the lock cannot be acquired.
func (l *defaultLock) TryAcquire() error {
	if err := l.open(); err != nil {
		return err
	}
	err := syscall.Flock(l.fd, syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		syscall.Close(l.fd)
	}
	if err == syscall.EWOULDBLOCK {
		return ErrLocked
	}
	return err
}

func (l *defaultLock) open() error {
	fd, err := syscall.Open(l.filename, syscall.O_CREAT|syscall.O_RDONLY, 0600)
	if err != nil {
		return err
	}
	l.fd = fd
	return nil
}
