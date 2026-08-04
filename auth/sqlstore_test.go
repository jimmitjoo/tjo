package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

func sqliteStore(t *testing.T) *SQLResetStore {
	t.Helper()

	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	// One writer, which is what SQLite wants and what the transaction in
	// Consume relies on for exclusivity.
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })

	s := NewSQLResetStore(db, DialectSQLite)
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s
}

func postgresStore(t *testing.T) *SQLResetStore {
	t.Helper()

	dsn := os.Getenv("TJO_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TJO_TEST_POSTGRES_DSN is not set")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("ping: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DROP TABLE IF EXISTS tjo_reset_tokens")
		db.Close()
	})

	if _, err := db.Exec("DROP TABLE IF EXISTS tjo_reset_tokens"); err != nil {
		t.Fatal(err)
	}
	s := NewSQLResetStore(db, DialectPostgres)
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s
}

func runStoreSuite(t *testing.T, store *SQLResetStore) {
	ctx := context.Background()

	t.Run("save and redeem", func(t *testing.T) {
		tok, err := NewResetToken("user-1", PurposePasswordReset, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Save(ctx, tok); err != nil {
			t.Fatal(err)
		}

		got, err := Redeem(ctx, store, tok.PlainText, PurposePasswordReset)
		if err != nil {
			t.Fatalf("Redeem: %v", err)
		}
		if got != "user-1" {
			t.Errorf("redeemed for %q", got)
		}

		if _, err := Redeem(ctx, store, tok.PlainText, PurposePasswordReset); !errors.Is(err, ErrInvalidReset) {
			t.Errorf("a consumed token was redeemed again: %v", err)
		}
	})

	// The reason this store exists, and the test needs care to be worth
	// anything.
	//
	// A first version launched 16 goroutines with no coordination and passed
	// against a deliberately broken SELECT-then-UPDATE implementation on both
	// engines: goroutine startup is staggered enough that the window between
	// the read and the write is never actually contended. A guard that cannot
	// observe the bug it exists for is worse than no guard.
	//
	// This one releases every goroutine from a closed channel so they arrive
	// together, and repeats the whole scenario, because one round is a coin
	// flip and thirty is not. Against the naive implementation it fails on
	// round 1 with two successful redemptions -- two people resetting the same
	// account's password.
	//
	// It only discriminates on PostgreSQL, and that is not a gap. On SQLite the
	// pool is one write connection and the writer is serialised, so the whole
	// read-then-write sequence inside a transaction is already exclusive: the
	// naive implementation is genuinely safe there. PostgreSQL has real
	// concurrency, so it needs the single-statement UPDATE ... RETURNING, and
	// that is what this proves.
	t.Run("concurrent redemption yields exactly one winner", func(t *testing.T) {
		const (
			rounds = 30
			racers = 24
		)

		totalWins := 0
		for round := 0; round < rounds; round++ {
			tok, err := NewResetToken(fmt.Sprintf("user-race-%d", round), PurposePasswordReset, time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Save(ctx, tok); err != nil {
				t.Fatal(err)
			}

			var (
				start = make(chan struct{})
				wg    sync.WaitGroup
				mu    sync.Mutex
				wins  int
			)
			for i := 0; i < racers; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					if _, err := Redeem(ctx, store, tok.PlainText, PurposePasswordReset); err == nil {
						mu.Lock()
						wins++
						mu.Unlock()
					}
				}()
			}
			close(start)
			wg.Wait()

			if wins != 1 {
				t.Fatalf("round %d: %d redemptions succeeded, want exactly 1 -- the claim is not atomic", round, wins)
			}
			totalWins += wins
		}

		if totalWins != rounds {
			t.Errorf("%d wins across %d rounds", totalWins, rounds)
		}
	})

	t.Run("expired tokens are not redeemable", func(t *testing.T) {
		tok, _ := NewResetToken("user-exp", PurposePasswordReset, -time.Minute)
		if err := store.Save(ctx, tok); err != nil {
			t.Fatal(err)
		}
		if _, err := Redeem(ctx, store, tok.PlainText, PurposePasswordReset); !errors.Is(err, ErrInvalidReset) {
			t.Errorf("an expired token was redeemed: %v", err)
		}
	})

	t.Run("purpose is enforced in SQL", func(t *testing.T) {
		tok, _ := NewResetToken("user-purpose", PurposeActivation, time.Hour)
		if err := store.Save(ctx, tok); err != nil {
			t.Fatal(err)
		}
		if _, err := Redeem(ctx, store, tok.PlainText, PurposePasswordReset); !errors.Is(err, ErrInvalidReset) {
			t.Errorf("an activation token was redeemed as a password reset: %v", err)
		}
	})

	t.Run("the plaintext is never stored", func(t *testing.T) {
		tok, _ := NewResetToken("user-plain", PurposePasswordReset, time.Hour)
		if err := store.Save(ctx, tok); err != nil {
			t.Fatal(err)
		}

		var n int
		err := store.db.QueryRow(store.rebind(
			`SELECT COUNT(*) FROM `+store.table+` WHERE CAST(hash AS TEXT) = ?`), tok.PlainText).Scan(&n)
		if err == nil && n > 0 {
			t.Error("the plaintext token was stored")
		}
	})

	t.Run("invalidate kills outstanding tokens", func(t *testing.T) {
		var toks []*ResetToken
		for i := 0; i < 3; i++ {
			tok, _ := NewResetToken("user-inv", PurposePasswordReset, time.Hour)
			if err := store.Save(ctx, tok); err != nil {
				t.Fatal(err)
			}
			toks = append(toks, tok)
		}

		if err := store.InvalidateUser(ctx, "user-inv", PurposePasswordReset); err != nil {
			t.Fatal(err)
		}
		for i, tok := range toks {
			if _, err := Redeem(ctx, store, tok.PlainText, PurposePasswordReset); !errors.Is(err, ErrInvalidReset) {
				t.Errorf("token %d survived invalidation: %v", i, err)
			}
		}
	})

	// Without this the table keeps every reset anyone ever requested.
	t.Run("expired rows can be deleted", func(t *testing.T) {
		tok, _ := NewResetToken("user-gc", PurposePasswordReset, -48*time.Hour)
		if err := store.Save(ctx, tok); err != nil {
			t.Fatal(err)
		}

		n, err := store.DeleteExpired(ctx, 24*time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			t.Error("DeleteExpired removed nothing")
		}
	})
}

func TestSQLResetStoreOnSQLite(t *testing.T) { runStoreSuite(t, sqliteStore(t)) }

// The same contract on PostgreSQL, where atomicity comes from UPDATE ...
// RETURNING rather than from a transaction over a serialised writer. Two
// different mechanisms, so testing one proves nothing about the other.
func TestSQLResetStoreOnPostgres(t *testing.T) { runStoreSuite(t, postgresStore(t)) }
