package database

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestVectorFormat(t *testing.T) {
	tests := map[string]struct {
		in   []float32
		want string
	}{
		"empty":    {nil, "[]"},
		"one":      {[]float32{1}, "[1]"},
		"three":    {[]float32{0.1, -0.25, 3}, "[0.1,-0.25,3]"},
		"negative": {[]float32{-0.0001}, "[-0.0001]"},
	}

	for name, tt := range tests {
		if got := Vector(tt.in); got != tt.want {
			t.Errorf("%s: Vector(%v) = %q, want %q", name, tt.in, got, tt.want)
		}
	}
}

// The embedding is bound, never interpolated. It arrives from a model, which
// makes it data, and a 1536-dimension vector pasted into a query string is also
// a query nothing can cache.
func TestNearestBindsTheEmbedding(t *testing.T) {
	qb := NewQueryBuilder(nil).WithDialect(DialectDollar).
		Table("documents").
		Where("organization_id", "=", "org-1").
		Nearest("embedding", []float32{0.1, 0.2}, Cosine).
		Limit(10)

	query, params, err := qb.ToSQL()
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(query, "embedding <=> $2::vector") {
		t.Fatalf("query does not order by cosine distance with a bound, cast parameter:\n%s", query)
	}
	if strings.Contains(query, "0.1") {
		t.Fatalf("the embedding was interpolated into the query:\n%s", query)
	}

	// The WHERE parameter comes first and the ORDER BY parameter second, which
	// is the order the placeholders appear in the text -- and the order the
	// dialect's rebind numbers them by. Getting this wrong swaps the tenant id
	// and the vector, which PostgreSQL then rejects in a way that looks like a
	// type error rather than an ordering bug.
	if len(params) != 2 {
		t.Fatalf("%d parameters, want 2: %v", len(params), params)
	}
	if params[0] != "org-1" {
		t.Fatalf("parameter 1 is %v, want the WHERE value", params[0])
	}
	if params[1] != "[0.1,0.2]" {
		t.Fatalf("parameter 2 is %v, want the embedding", params[1])
	}
}

func TestNearestPerDialect(t *testing.T) {
	postgres, _, err := NewQueryBuilder(nil).WithDialect(DialectDollar).
		Table("docs").Nearest("embedding", []float32{1, 2}, L2).ToSQL()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(postgres, "embedding <-> $1::vector ASC") {
		t.Errorf("postgres L2:\n%s", postgres)
	}

	sqlite, _, err := NewQueryBuilder(nil).WithDialect(DialectQuestion).
		Table("docs").Nearest("embedding", []float32{1, 2}, Cosine).ToSQL()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sqlite, "vec_distance_cosine(embedding, ?) ASC") {
		t.Errorf("sqlite cosine:\n%s", sqlite)
	}
}

func TestNearestWithinFiltersRatherThanOrders(t *testing.T) {
	query, params, err := NewQueryBuilder(nil).WithDialect(DialectDollar).
		Table("questions").
		Where("archived", "=", false).
		NearestWithin("embedding", []float32{0.5}, Cosine, 0.15).
		ToSQL()
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(query, "embedding <=> $2::vector < $3") {
		t.Fatalf("query does not filter on distance:\n%s", query)
	}
	if strings.Contains(query, "ORDER BY") {
		t.Fatalf("NearestWithin added an ordering:\n%s", query)
	}
	if len(params) != 3 || params[2] != 0.15 {
		t.Fatalf("parameters = %v, want the threshold last", params)
	}
}

// A column name reaches SQL as text, so it is checked. The embedding never
// does, so it is not.
func TestNearestRefusesAnUnusableColumn(t *testing.T) {
	_, _, err := NewQueryBuilder(nil).Table("docs").
		Nearest("embedding; DROP TABLE docs", []float32{1}, Cosine).ToSQL()
	if err == nil {
		t.Fatal("an injected column name was accepted")
	}

	_, _, err = NewQueryBuilder(nil).Table("docs").
		Nearest("embedding", nil, Cosine).ToSQL()
	if err == nil {
		t.Fatal("an empty embedding was accepted")
	}
}

// The whole point of keeping the vector in the primary database: the tenancy
// filter and the similarity ordering are one query. Run against SQLite without
// the extension, so it proves the SQL composes -- the distance function is what
// sqlite-vec provides, and a database without it fails at execution, not at
// build time.
func TestNearestComposesWithFiltersAndPaging(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "vec.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	query, params, err := NewQueryBuilder(db).
		Table("documents").
		Select("id", "title").
		Where("organization_id", "=", "org-7").
		Where("published", "=", true).
		Nearest("embedding", []float32{0.1, 0.9}, Cosine).
		Paginate(2, 10).
		ToSQL()
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		"SELECT id, title FROM documents",
		"WHERE organization_id = ?",
		"AND published = ?",
		"ORDER BY vec_distance_cosine(embedding, ?) ASC",
		"LIMIT 10",
		"OFFSET 10",
	} {
		if !strings.Contains(query, want) {
			t.Errorf("query is missing %q:\n%s", want, query)
		}
	}

	if len(params) != 3 {
		t.Fatalf("%d parameters, want 3: %v", len(params), params)
	}
	if params[2] != "[0.1,0.9]" {
		t.Fatalf("the embedding is parameter %v, want it last", params)
	}
}
