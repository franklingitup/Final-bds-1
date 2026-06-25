package events

import "time"

// RetryPolicy controls redelivery of events whose handler returns an error.
// Delivery attempts use exponential backoff, capped at MaxDelay. After
// MaxAttempts deliveries the event is dead-lettered.
type RetryPolicy struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
}

// DefaultRetryPolicy returns a sensible production default: 5 attempts with
// exponential backoff from 1s to 30s.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxAttempts: 5,
		BaseDelay:   time.Second,
		MaxDelay:    30 * time.Second,
	}
}

func (p RetryPolicy) normalized() RetryPolicy {
	if p.MaxAttempts <= 0 {
		p.MaxAttempts = DefaultRetryPolicy().MaxAttempts
	}
	if p.BaseDelay <= 0 {
		p.BaseDelay = DefaultRetryPolicy().BaseDelay
	}
	if p.MaxDelay < p.BaseDelay {
		p.MaxDelay = DefaultRetryPolicy().MaxDelay
		if p.MaxDelay < p.BaseDelay {
			p.MaxDelay = p.BaseDelay
		}
	}
	return p
}

// backoff returns the delay before the given attempt number (1-based). Attempt 1
// (the redelivery after the first failure) waits BaseDelay; each subsequent
// attempt doubles, capped at MaxDelay.
func (p RetryPolicy) backoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := p.BaseDelay
	for i := 1; i < attempt; i++ {
		delay *= 2
		if delay >= p.MaxDelay {
			return p.MaxDelay
		}
	}
	if delay > p.MaxDelay {
		return p.MaxDelay
	}
	return delay
}

// backoffSchedule returns the per-attempt backoff durations supplied to the
// JetStream consumer's BackOff: the MaxAttempts-1 gaps between delivery
// attempts. Returns nil when no redelivery is configured (MaxAttempts <= 1), so
// the consumer's BackOff stays within the MaxDeliver bound.
func (p RetryPolicy) backoffSchedule() []time.Duration {
	n := p.MaxAttempts - 1
	if n < 1 {
		return nil
	}
	out := make([]time.Duration, n)
	for i := 0; i < n; i++ {
		out[i] = p.backoff(i + 1)
	}
	return out
}
