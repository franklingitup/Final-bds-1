package database

import (
	"testing"
	"time"
)

func TestModel_Cursor(t *testing.T) {
	now := time.Now().UTC()
	m := Model{ID: "id-1", CreatedAt: now}
	c := m.Cursor()
	if c.ID != "id-1" || !c.CreatedAt.Equal(now) {
		t.Errorf("Cursor() = %+v", c)
	}
}
