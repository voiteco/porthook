// SPDX-License-Identifier: Apache-2.0

package agent

import (
	"crypto/rand"
	"errors"
	"math/big"
	"time"
)

type permanentError struct {
	err error
}

func (e permanentError) Error() string {
	return e.err.Error()
}

func (e permanentError) Unwrap() error {
	return e.err
}

func permanent(err error) error {
	if err == nil {
		return nil
	}
	return permanentError{err: err}
}

func isPermanent(err error) bool {
	var target permanentError
	return errors.As(err, &target)
}

func reconnectDelay(attempt int, initial, max, jitter time.Duration, jitterFunc func(time.Duration) time.Duration) time.Duration {
	if initial <= 0 {
		initial = defaultReconnectInitialDelay
	}
	if max <= 0 {
		max = initial
	}
	if max < initial {
		max = initial
	}
	if attempt < 0 {
		attempt = 0
	}

	delay := initial
	for i := 0; i < attempt && delay < max; i++ {
		delay *= 2
		if delay > max {
			delay = max
		}
	}

	if jitter <= 0 || jitterFunc == nil {
		return delay
	}
	if jitter > delay {
		jitter = delay
	}

	delay += jitterFunc(jitter)
	if delay > max {
		return max
	}
	return delay
}

func randomReconnectJitter(limit time.Duration) time.Duration {
	if limit <= 0 {
		return 0
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(limit)+1))
	if err != nil {
		return 0
	}
	return time.Duration(n.Int64())
}
