// One-off tool: sets a correct argon2id password hash for a user directly in
// the database, using the real HashPassword function from the auth service.
// Usage (from backend/): go run ./services/auth/cmd/set-password/
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5"

	auth "github.com/bdsplatform/platform/backend/services/auth/internal"
)

const (
	targetEmail = "jothi.franklin@bastiondatasolutions.com"
	targetPass  = "TempPass123!"
	databaseURL = "postgres://platform:platform@127.0.0.1:5434/platform?sslmode=disable"
)

func main() {
	ctx := context.Background()

	// 1. Generate the hash using the real auth service function.
	hash, err := auth.HashPassword(targetPass)
	if err != nil {
		fmt.Fprintf(os.Stderr, "HashPassword error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Generated hash: %s\n\n", hash)

	// 2. Connect to Postgres.
	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Connect error: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close(ctx)
	fmt.Println("Connected to database.")

	// 3. UPDATE: set password_hash, clear lock fields.
	tag, err := conn.Exec(ctx,
		`UPDATE users
		    SET password_hash          = $1,
		        failed_login_attempts  = 0,
		        locked_until           = NULL
		  WHERE email = $2`,
		hash, targetEmail,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "UPDATE error: %v\n", err)
		os.Exit(1)
	}
	if tag.RowsAffected() == 0 {
		fmt.Fprintf(os.Stderr, "UPDATE matched 0 rows — email %q not found\n", targetEmail)
		os.Exit(1)
	}
	fmt.Printf("UPDATE affected %d row(s).\n\n", tag.RowsAffected())

	// 4. SELECT back to confirm what is stored.
	var storedHash string
	var failedAttempts int
	err = conn.QueryRow(ctx,
		`SELECT password_hash, failed_login_attempts
		   FROM users
		  WHERE email = $1`,
		targetEmail,
	).Scan(&storedHash, &failedAttempts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "SELECT error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Stored password_hash:         %s\n", storedHash)
	fmt.Printf("Stored failed_login_attempts: %d\n\n", failedAttempts)

	if storedHash == hash {
		fmt.Printf("SUCCESS — hash confirmed in database: %s\n", storedHash)
	} else {
		fmt.Fprintf(os.Stderr, "MISMATCH — stored hash differs from generated hash\n")
		os.Exit(1)
	}
}
