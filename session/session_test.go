package session

import (
	"database/sql"
	"fmt"
	"reflect"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/alexedwards/scs/v2"
)

func TestSession_InitSession(t *testing.T) {

	g := &Session{
		CookieLifetime: "100",
		CookiePersist:  "true",
		CookieName:     "tjo",
		CookieDomain:   "localhost",
		SessionType:    "cookie",
	}

	var sm *scs.SessionManager

	ses, err := g.InitSession()
	if err != nil {
		t.Fatal(err)
	}

	var sessKind reflect.Kind
	var sessType reflect.Type

	rv := reflect.ValueOf(ses)

	for rv.Kind() == reflect.Ptr || rv.Kind() == reflect.Interface {
		fmt.Println("For loop:", rv.Kind(), rv.Type(), rv)
		sessKind = rv.Kind()
		sessType = rv.Type()
		rv = rv.Elem()
	}

	if !rv.IsValid() {
		t.Error("invalid type or kind; kind:", rv.Kind(), "type:", rv.Type())
	}

	if sessKind != reflect.ValueOf(sm).Kind() {
		t.Error("wrong kind returned testing cookie session. Expected", reflect.ValueOf(sm).Kind(), "and got", sessKind)
	}

	if sessType != reflect.ValueOf(sm).Type() {
		t.Error("wrong type returned testing cookie session. Expected", reflect.ValueOf(sm).Type(), "and got", sessType)
	}
}

// TestSessionTypeVocabularyMatchesConfig covers issue #10. config.Validate
// accepts cookie/redis/database/badger while this switch understood
// redis/mysql/mariadb/postgres/postgresql, so the two overlapped only on
// "redis". SESSION_TYPE=database fell through to scs's in-memory store:
// sessions vanished on restart and broke behind a second replica, silently.
func TestSessionTypeVocabularyMatchesConfig(t *testing.T) {
	tests := []struct {
		name        string
		sessionType string
		dbType      string
		hasPool     bool
		wantErr     bool
		wantMemory  bool
	}{
		{name: "cookie is in-memory by design", sessionType: "cookie", wantMemory: true},
		{name: "database+postgres resolves to a store", sessionType: "database", dbType: "postgres", hasPool: true},
		{name: "database+mysql resolves to a store", sessionType: "database", dbType: "mysql", hasPool: true},
		{name: "database without a pool errors", sessionType: "database", dbType: "postgres", wantErr: true},
		{name: "database with an unsupported driver errors", sessionType: "database", dbType: "sqlite3", hasPool: true, wantErr: true},
		{name: "badger is not a session store", sessionType: "badger", wantErr: true},
		{name: "redis without a pool errors", sessionType: "redis", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Session{
				CookieLifetime: "1440",
				CookieName:     "tjo",
				SessionType:    tt.sessionType,
				DBType:         tt.dbType,
			}
			if tt.hasPool {
				db, err := sql.Open("sqlite3", ":memory:")
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
				s.DBPool = db
			}

			mgr, err := s.InitSession()

			if tt.wantErr {
				assert.Error(t, err, "expected a loud failure rather than a silent in-memory fallback")
				return
			}
			require.NoError(t, err)

			if tt.wantMemory {
				assert.NotNil(t, mgr.Store)
			} else {
				assert.NotNil(t, mgr.Store, "a database-backed store should have been selected")
			}
		})
	}
}

// TestCookieLifetimeIsApplied covers the other half of #10: COOKIE_LIFETIME was
// parsed into session.Lifetime and then immediately overwritten by the
// hardcoded 30 minute default, so every app got 30 minute sessions regardless.
func TestCookieLifetimeIsApplied(t *testing.T) {
	s := &Session{
		CookieLifetime: "1440",
		CookieName:     "tjo",
		SessionType:    "cookie",
	}

	mgr, err := s.InitSession()
	require.NoError(t, err)

	assert.Equal(t, 1440*time.Minute, mgr.Lifetime,
		"COOKIE_LIFETIME was ignored in favour of the hardcoded default")
}
