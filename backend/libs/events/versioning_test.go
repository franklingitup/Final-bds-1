package events

import "testing"

func TestSubjectRoundTrip(t *testing.T) {
	e, _ := New("deployment.succeeded", 2, "org", nil)
	subject := Subject("evt", e)
	if subject != "evt.deployment.succeeded.v2" {
		t.Fatalf("subject = %q", subject)
	}

	typ, ver, ok := ParseSubject("evt", subject)
	if !ok || typ != "deployment.succeeded" || ver != 2 {
		t.Errorf("ParseSubject = (%q, %d, %v)", typ, ver, ok)
	}
}

func TestParseSubject_Invalid(t *testing.T) {
	if _, _, ok := ParseSubject("evt", "other.deployment.v1"); ok {
		t.Error("wrong prefix should not parse")
	}
	if _, _, ok := ParseSubject("evt", "evt.deployment.succeeded"); ok {
		t.Error("missing version should not parse")
	}
	if _, _, ok := ParseSubject("evt", "evt.deployment.vX"); ok {
		t.Error("non-numeric version should not parse")
	}
}

func TestRegistry_UpgradeChain(t *testing.T) {
	reg := NewRegistry()
	// v1 -> v2: add a field; v2 -> v3: rename, etc. (payload mutation omitted).
	reg.Register("deployment.succeeded", 1, func(e Envelope) (Envelope, error) {
		e.Version = 2
		return e, nil
	})
	reg.Register("deployment.succeeded", 2, func(e Envelope) (Envelope, error) {
		e.Version = 3
		return e, nil
	})

	v1, _ := New("deployment.succeeded", 1, "org", nil)
	upgraded, err := reg.Upgrade(v1)
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	if upgraded.Version != 3 {
		t.Errorf("upgraded to v%d, want v3", upgraded.Version)
	}
}

func TestRegistry_NoUpcasterIsNoop(t *testing.T) {
	reg := NewRegistry()
	e, _ := New("x.y", 5, "org", nil)
	out, err := reg.Upgrade(e)
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	if out.Version != 5 {
		t.Errorf("version changed unexpectedly to %d", out.Version)
	}
}

func TestRegistry_BadUpcasterVersion(t *testing.T) {
	reg := NewRegistry()
	reg.Register("bad", 1, func(e Envelope) (Envelope, error) {
		e.Version = 5 // must be 2
		return e, nil
	})
	e, _ := New("bad", 1, "org", nil)
	if _, err := reg.Upgrade(e); err == nil {
		t.Error("expected error when upcaster skips versions")
	}
}
