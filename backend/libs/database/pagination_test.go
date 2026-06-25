package database

import (
	"testing"
	"time"
)

func TestPageRequest_Normalize(t *testing.T) {
	cases := []struct {
		in   int
		want int
	}{
		{in: 0, want: DefaultPageLimit},
		{in: -5, want: DefaultPageLimit},
		{in: 10, want: 10},
		{in: MaxPageLimit + 50, want: MaxPageLimit},
	}
	for _, c := range cases {
		if got := (PageRequest{Limit: c.in}).Normalize().Limit; got != c.want {
			t.Errorf("Normalize(%d).Limit = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestCursor_RoundTrip(t *testing.T) {
	orig := Cursor{CreatedAt: time.Now().UTC().Truncate(time.Microsecond), ID: "abc-123"}
	token := EncodeCursor(orig)
	if token == "" {
		t.Fatal("expected non-empty token")
	}
	got, err := DecodeCursor(token)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.CreatedAt.Equal(orig.CreatedAt) || got.ID != orig.ID {
		t.Errorf("round trip = %+v, want %+v", got, orig)
	}
}

func TestDecodeCursor_Empty(t *testing.T) {
	c, err := DecodeCursor("")
	if err != nil {
		t.Fatalf("decode empty: %v", err)
	}
	if !c.IsZero() {
		t.Error("empty token should decode to zero cursor")
	}
}

func TestDecodeCursor_Invalid(t *testing.T) {
	if _, err := DecodeCursor("!!!not-base64!!!"); err == nil {
		t.Error("expected error for malformed cursor")
	}
}

func TestBuildPage_NextCursorWhenExtraRow(t *testing.T) {
	type row struct {
		id string
		ts time.Time
	}
	key := func(r row) Cursor { return Cursor{CreatedAt: r.ts, ID: r.id} }

	base := time.Now().UTC()
	items := []row{
		{id: "1", ts: base},
		{id: "2", ts: base.Add(-time.Minute)},
		{id: "3", ts: base.Add(-2 * time.Minute)}, // the limit+1 sentinel
	}

	page := BuildPage(items, 2, key)
	if len(page.Items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(page.Items))
	}
	if page.NextCursor == "" {
		t.Fatal("expected a next cursor")
	}
	// Next cursor must point at the last returned item (id "2"), not the sentinel.
	c, _ := DecodeCursor(page.NextCursor)
	if c.ID != "2" {
		t.Errorf("next cursor id = %q, want 2", c.ID)
	}
}

func TestBuildPage_NoNextCursor(t *testing.T) {
	type row struct{ id string }
	key := func(r row) Cursor { return Cursor{ID: r.id} }
	page := BuildPage([]row{{id: "1"}, {id: "2"}}, 5, key)
	if page.NextCursor != "" {
		t.Errorf("expected no next cursor, got %q", page.NextCursor)
	}
	if len(page.Items) != 2 {
		t.Errorf("len(items) = %d, want 2", len(page.Items))
	}
}
