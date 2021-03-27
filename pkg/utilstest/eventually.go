package utilstest

import (
	"testing"
	"time"
)

// EventuallyResult contains the information about eventually status.
type EventuallyResult struct {
	lastError error
	attempts  int
}

// Attempts returns the number of attrempts for this eventually.
func (e *EventuallyResult) Attempts() int {
	return e.attempts
}

// Error returns last recorded error.
func (e *EventuallyResult) Error() error {
	return e.lastError
}

// Eventually executes a function every interval for forMaximumDuration, until firs time the block succeeds.
func Eventually(t *testing.T, f func() error, interval, forMaximumDuration time.Duration) *EventuallyResult {
	endAt := time.Now().Add(forMaximumDuration)
	var attempts int
	var lastError error
	for {
		lastError = f()
		attempts = attempts + 1
		if lastError == nil {
			break
		}
		if endAt.Sub(time.Now()).Seconds() < 0 {
			break
		}
		<-time.After(interval)
	}
	return &EventuallyResult{
		lastError: lastError,
		attempts:  attempts,
	}
}

// MustEventually must complete eventually execution with success within given duration, otherwise fail the test immediately.
func MustEventually(t *testing.T, f func() error, interval, forMaximumDuration time.Duration) {
	result := Eventually(t, f, interval, forMaximumDuration)
	if result.Error() != nil {
		t.Fatal("Attempted", result.Attempts(), "time(s), reason:", result.Error())
	}
}

// MustEventuallyWithDefaults must complete eventually execution with success within given duration, otherwise fail the test immediately.
// Uses default timeouts.
func MustEventuallyWithDefaults(t *testing.T, f func() error) {
	MustEventually(t, f, time.Duration(time.Millisecond*100), time.Duration(time.Second*5))
}
