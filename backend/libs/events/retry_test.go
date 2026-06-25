package events

import (
	"testing"
	"time"
)

func TestRetryPolicy_BackoffGrowsAndCaps(t *testing.T) {
	p := RetryPolicy{MaxAttempts: 6, BaseDelay: time.Second, MaxDelay: 8 * time.Second}

	want := []time.Duration{
		1 * time.Second, // attempt 1
		2 * time.Second, // attempt 2
		4 * time.Second, // attempt 3
		8 * time.Second, // attempt 4 (cap)
		8 * time.Second, // attempt 5 (cap)
	}
	for i, w := range want {
		if got := p.backoff(i + 1); got != w {
			t.Errorf("backoff(%d) = %v, want %v", i+1, got, w)
		}
	}
}

func TestRetryPolicy_BackoffScheduleLength(t *testing.T) {
	p := RetryPolicy{MaxAttempts: 5, BaseDelay: time.Second, MaxDelay: 30 * time.Second}
	sched := p.backoffSchedule()
	if len(sched) != 4 { // MaxAttempts - 1 gaps
		t.Fatalf("schedule length = %d, want 4", len(sched))
	}
	if sched[0] != time.Second {
		t.Errorf("first delay = %v, want 1s", sched[0])
	}
}

func TestRetryPolicy_NormalizeDefaults(t *testing.T) {
	n := RetryPolicy{}.normalized()
	def := DefaultRetryPolicy()
	if n.MaxAttempts != def.MaxAttempts || n.BaseDelay != def.BaseDelay || n.MaxDelay != def.MaxDelay {
		t.Errorf("normalized zero policy = %+v, want %+v", n, def)
	}
}

func TestDLQSubject(t *testing.T) {
	e, _ := New("deployment.succeeded", 2, "org", nil)
	if got := dlqSubject(e); got != "dlq.deployment.succeeded.v2" {
		t.Errorf("dlqSubject = %q", got)
	}
}
