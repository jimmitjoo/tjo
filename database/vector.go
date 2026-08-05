package database

import (
	"fmt"
	"strconv"
	"strings"
)

// Vector search, for the case where the vector lives in the primary database.
//
// That case is settled in a way the rest of the AI stack is not. pgvector is
// the default answer on PostgreSQL and sqlite-vec is the one on SQLite, both
// store the embedding next to the row it describes, and neither requires a
// second datastore to operate. A framework can support that without betting on
// anything: it is a column type and three operators.
//
// What is deliberately absent is index management. `CREATE INDEX ... USING
// hnsw (embedding vector_cosine_ops) WITH (m = 16, ef_construction = 64)` is a
// tuning decision that depends on the data, the recall target and the memory
// budget, and a query builder that guessed at it would be guessing on behalf of
// somebody who knows more than it does. Write the index in a migration.

// Distance is how similarity between two vectors is measured.
//
// The choice is not free: it must match the operator class the index was built
// with, or the index is not used and the query silently becomes a sequential
// scan over every row. That is the single most common way vector search
// "works" in development and falls over in production.
type Distance int

const (
	// Cosine ignores magnitude and compares direction. The right default for
	// text embeddings, and what OpenAI and most others document.
	Cosine Distance = iota

	// L2 is Euclidean distance. For embeddings where magnitude is meaningful.
	L2

	// InnerProduct is the negative dot product. Fastest, and correct only for
	// normalised vectors -- on unnormalised ones it ranks by magnitude as much
	// as by similarity.
	InnerProduct
)

// operator returns the pgvector operator for a distance.
func (d Distance) operator() string {
	switch d {
	case L2:
		return "<->"
	case InnerProduct:
		return "<#>"
	default:
		return "<=>"
	}
}

// sqliteFunc returns the sqlite-vec function for a distance.
func (d Distance) sqliteFunc() string {
	switch d {
	case Cosine:
		return "vec_distance_cosine"
	case InnerProduct:
		// sqlite-vec has no inner-product function. L2 over normalised vectors
		// ranks identically, so this degrades to that rather than pretending.
		return "vec_distance_l2"
	default:
		return "vec_distance_l2"
	}
}

// Vector formats an embedding for a query parameter.
//
// Both pgvector and sqlite-vec accept the same text form -- "[0.1,0.2,0.3]" --
// so one function covers both. Formatted with 'g' rather than a fixed
// precision: an embedding is float32 and printing more digits than it has
// stores noise, printing fewer changes the distance.
func Vector(embedding []float32) string {
	var b strings.Builder
	b.Grow(len(embedding) * 12)

	b.WriteByte('[')
	for i, v := range embedding {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(v), 'g', -1, 32))
	}
	b.WriteByte(']')

	return b.String()
}

// Nearest orders results by distance from an embedding, closest first.
//
// It is the whole of vector search in a query builder:
//
//	rows, err := database.NewQueryBuilder(db).WithDialect(database.DialectDollar).
//	    Table("documents").
//	    Where("organization_id", "=", orgID).
//	    Nearest("embedding", queryVector, database.Cosine).
//	    Limit(10).
//	    Get()
//
// The filter and the ordering compose, which is the reason to keep the vector
// in the primary database at all: a separate vector store cannot answer "the
// ten nearest documents *belonging to this organization*" without either
// duplicating the tenancy model or over-fetching and filtering afterwards.
//
// The embedding is a bound parameter, not interpolated. It arrives from a model
// and is therefore data.
func (qb *QueryBuilder) Nearest(column string, embedding []float32, distance Distance) *QueryBuilder {
	if !isValidIdentifier(column) {
		qb.err = fmt.Errorf("invalid column name in Nearest: %q", column)
		return qb
	}
	if len(embedding) == 0 {
		qb.err = fmt.Errorf("Nearest: the embedding is empty")
		return qb
	}

	var expression string
	if qb.dialect == DialectDollar {
		// pgvector: the operator needs the parameter cast, because a bound
		// string is text until told otherwise and there is no text <=> vector.
		expression = fmt.Sprintf("%s %s ?::vector", column, distance.operator())
	} else {
		expression = fmt.Sprintf("%s(%s, ?)", distance.sqliteFunc(), column)
	}

	qb.orderBy = append(qb.orderBy, expression+" ASC")
	qb.orderByParams = append(qb.orderByParams, Vector(embedding))

	return qb
}

// NearestWithin filters to rows within a distance, rather than ordering by it.
//
// Useful when "similar enough" is a threshold rather than a ranking -- a
// deduplication check, or a "did anybody already ask this" lookup. The
// threshold is in the units of the chosen distance, so it is not portable
// between them: cosine distance runs 0 to 2, L2 is unbounded.
func (qb *QueryBuilder) NearestWithin(column string, embedding []float32, distance Distance, threshold float64) *QueryBuilder {
	if !isValidIdentifier(column) {
		qb.err = fmt.Errorf("invalid column name in NearestWithin: %q", column)
		return qb
	}
	if len(embedding) == 0 {
		qb.err = fmt.Errorf("NearestWithin: the embedding is empty")
		return qb
	}

	var expression string
	if qb.dialect == DialectDollar {
		expression = fmt.Sprintf("%s %s ?::vector", column, distance.operator())
	} else {
		expression = fmt.Sprintf("%s(%s, ?)", distance.sqliteFunc(), column)
	}

	qb.whereConds = append(qb.whereConds, whereCondition{
		logic:  "AND",
		raw:    expression + " < ?",
		params: []interface{}{Vector(embedding), threshold},
	})

	return qb
}
