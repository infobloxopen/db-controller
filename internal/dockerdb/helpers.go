package dockerdb

import (
	"testing"
	"time"
)

// RetryFn retries a function until it succeeds or the timeout is reached
func RetryFn(t *testing.T, mightFail func() error, retryInterval, timeout time.Duration) error {
	if t != nil {
		t.Helper()
	}

	var err error
	startTime := time.Now()

	for time.Since(startTime) < timeout {
		err = mightFail()
		if err == nil {
			return nil
		}
		time.Sleep(retryInterval)
	}

	// If we reach here, the function did not succeed within the timeout
	if t != nil {
		t.Logf("retry_did_not_succeed within %s: %v", timeout, err)
	}
	return err
}
