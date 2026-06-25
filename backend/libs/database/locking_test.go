package database

import (
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestExpectAffected(t *testing.T) {
	if err := expectAffected(pgconn.NewCommandTag("UPDATE 1")); err != nil {
		t.Errorf("one row affected should succeed, got %v", err)
	}
	err := expectAffected(pgconn.NewCommandTag("UPDATE 0"))
	if err != ErrOptimisticLock {
		t.Errorf("zero rows affected should be ErrOptimisticLock, got %v", err)
	}
}

func TestIsOptimisticLock(t *testing.T) {
	if !IsOptimisticLock(ErrOptimisticLock) {
		t.Error("ErrOptimisticLock should be recognized")
	}
	if IsOptimisticLock(nil) {
		t.Error("nil is not an optimistic lock error")
	}
}
