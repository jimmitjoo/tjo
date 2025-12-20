package database

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueryBuilder_Select(t *testing.T) {
	qb := NewQueryBuilder(nil)
	
	t.Run("select all", func(t *testing.T) {
		sql, params, err := qb.Table("users").ToSQL()
		require.NoError(t, err)
		assert.Equal(t, "SELECT * FROM users", sql)
		assert.Empty(t, params)
	})
	
	t.Run("select specific columns", func(t *testing.T) {
		qb = NewQueryBuilder(nil)
		sql, params, err := qb.Table("users").Select("id", "name", "email").ToSQL()
		require.NoError(t, err)
		assert.Equal(t, "SELECT id, name, email FROM users", sql)
		assert.Empty(t, params)
	})
}

func TestQueryBuilder_Where(t *testing.T) {
	qb := NewQueryBuilder(nil)
	
	t.Run("single where condition", func(t *testing.T) {
		sql, params, err := qb.Table("users").Where("id", "=", 1).ToSQL()
		require.NoError(t, err)
		assert.Equal(t, "SELECT * FROM users WHERE id = ?", sql)
		assert.Equal(t, []interface{}{1}, params)
	})
	
	t.Run("multiple where conditions", func(t *testing.T) {
		qb = NewQueryBuilder(nil)
		sql, params, err := qb.Table("users").
			Where("id", ">", 1).
			Where("status", "=", "active").
			ToSQL()
		require.NoError(t, err)
		assert.Equal(t, "SELECT * FROM users WHERE id > ? AND status = ?", sql)
		assert.Equal(t, []interface{}{1, "active"}, params)
	})
	
	t.Run("or where condition", func(t *testing.T) {
		qb = NewQueryBuilder(nil)
		sql, params, err := qb.Table("users").
			Where("id", "=", 1).
			OrWhere("id", "=", 2).
			ToSQL()
		require.NoError(t, err)
		assert.Equal(t, "SELECT * FROM users WHERE id = ? OR id = ?", sql)
		assert.Equal(t, []interface{}{1, 2}, params)
	})
	
	t.Run("where in", func(t *testing.T) {
		qb = NewQueryBuilder(nil)
		sql, params, err := qb.Table("users").
			WhereIn("id", []interface{}{1, 2, 3}).
			ToSQL()
		require.NoError(t, err)
		assert.Equal(t, "SELECT * FROM users WHERE id IN (?, ?, ?)", sql)
		assert.Equal(t, []interface{}{1, 2, 3}, params) // Parameters now properly handled
	})
	
	t.Run("where between", func(t *testing.T) {
		qb = NewQueryBuilder(nil)
		sql, params, err := qb.Table("users").
			WhereBetween("age", 18, 65).
			ToSQL()
		require.NoError(t, err)
		assert.Equal(t, "SELECT * FROM users WHERE age BETWEEN ? AND ?", sql)
		assert.Equal(t, []interface{}{18, 65}, params) // Parameters now properly handled for security
	})
	
	t.Run("where null", func(t *testing.T) {
		qb = NewQueryBuilder(nil)
		sql, params, err := qb.Table("users").
			WhereNull("deleted_at").
			ToSQL()
		require.NoError(t, err)
		assert.Equal(t, "SELECT * FROM users WHERE deleted_at IS NULL", sql)
		assert.Empty(t, params)
	})
	
	t.Run("where not null", func(t *testing.T) {
		qb = NewQueryBuilder(nil)
		sql, params, err := qb.Table("users").
			WhereNotNull("email").
			ToSQL()
		require.NoError(t, err)
		assert.Equal(t, "SELECT * FROM users WHERE email IS NOT NULL", sql)
		assert.Empty(t, params)
	})
}

func TestQueryBuilder_Joins(t *testing.T) {
	qb := NewQueryBuilder(nil)
	
	t.Run("inner join", func(t *testing.T) {
		sql, params, err := qb.Table("users").
			Join("profiles", "users.id = profiles.user_id").
			ToSQL()
		require.NoError(t, err)
		assert.Equal(t, "SELECT * FROM users INNER JOIN profiles ON users.id = profiles.user_id", sql)
		assert.Empty(t, params)
	})
	
	t.Run("left join", func(t *testing.T) {
		qb = NewQueryBuilder(nil)
		sql, params, err := qb.Table("users").
			LeftJoin("profiles", "users.id = profiles.user_id").
			ToSQL()
		require.NoError(t, err)
		assert.Equal(t, "SELECT * FROM users LEFT JOIN profiles ON users.id = profiles.user_id", sql)
		assert.Empty(t, params)
	})
	
	t.Run("right join", func(t *testing.T) {
		qb = NewQueryBuilder(nil)
		sql, params, err := qb.Table("users").
			RightJoin("profiles", "users.id = profiles.user_id").
			ToSQL()
		require.NoError(t, err)
		assert.Equal(t, "SELECT * FROM users RIGHT JOIN profiles ON users.id = profiles.user_id", sql)
		assert.Empty(t, params)
	})
	
	t.Run("multiple joins", func(t *testing.T) {
		qb = NewQueryBuilder(nil)
		sql, params, err := qb.Table("users").
			Join("profiles", "users.id = profiles.user_id").
			LeftJoin("orders", "users.id = orders.user_id").
			ToSQL()
		require.NoError(t, err)
		expected := "SELECT * FROM users INNER JOIN profiles ON users.id = profiles.user_id LEFT JOIN orders ON users.id = orders.user_id"
		assert.Equal(t, expected, sql)
		assert.Empty(t, params)
	})
}

func TestQueryBuilder_OrderGroupBy(t *testing.T) {
	qb := NewQueryBuilder(nil)
	
	t.Run("order by ascending", func(t *testing.T) {
		sql, params, err := qb.Table("users").
			OrderBy("name", "ASC").
			ToSQL()
		require.NoError(t, err)
		assert.Equal(t, "SELECT * FROM users ORDER BY name ASC", sql)
		assert.Empty(t, params)
	})
	
	t.Run("order by descending", func(t *testing.T) {
		qb = NewQueryBuilder(nil)
		sql, params, err := qb.Table("users").
			OrderBy("created_at", "DESC").
			ToSQL()
		require.NoError(t, err)
		assert.Equal(t, "SELECT * FROM users ORDER BY created_at DESC", sql)
		assert.Empty(t, params)
	})
	
	t.Run("multiple order by", func(t *testing.T) {
		qb = NewQueryBuilder(nil)
		sql, params, err := qb.Table("users").
			OrderBy("status", "ASC").
			OrderBy("created_at", "DESC").
			ToSQL()
		require.NoError(t, err)
		assert.Equal(t, "SELECT * FROM users ORDER BY status ASC, created_at DESC", sql)
		assert.Empty(t, params)
	})
	
	t.Run("group by", func(t *testing.T) {
		qb = NewQueryBuilder(nil)
		sql, params, err := qb.Table("orders").
			Select("user_id", "COUNT(*)").
			GroupBy("user_id").
			ToSQL()
		require.NoError(t, err)
		assert.Equal(t, "SELECT user_id, COUNT(*) FROM orders GROUP BY user_id", sql)
		assert.Empty(t, params)
	})
	
	t.Run("having", func(t *testing.T) {
		qb = NewQueryBuilder(nil)
		sql, params, err := qb.Table("orders").
			Select("user_id", "COUNT(*)").
			GroupBy("user_id").
			Having("COUNT(*)", ">", 5).
			ToSQL()
		require.NoError(t, err)
		assert.Equal(t, "SELECT user_id, COUNT(*) FROM orders GROUP BY user_id HAVING COUNT(*) > ?", sql)
		assert.Equal(t, []interface{}{5}, params)
	})
}

func TestQueryBuilder_LimitOffset(t *testing.T) {
	qb := NewQueryBuilder(nil)
	
	t.Run("limit", func(t *testing.T) {
		sql, params, err := qb.Table("users").Limit(10).ToSQL()
		require.NoError(t, err)
		assert.Equal(t, "SELECT * FROM users LIMIT 10", sql)
		assert.Empty(t, params)
	})
	
	t.Run("limit with offset", func(t *testing.T) {
		qb = NewQueryBuilder(nil)
		sql, params, err := qb.Table("users").Limit(10).Offset(20).ToSQL()
		require.NoError(t, err)
		assert.Equal(t, "SELECT * FROM users LIMIT 10 OFFSET 20", sql)
		assert.Empty(t, params)
	})
	
	t.Run("paginate", func(t *testing.T) {
		qb = NewQueryBuilder(nil)
		sql, params, err := qb.Table("users").Paginate(2, 15).ToSQL()
		require.NoError(t, err)
		assert.Equal(t, "SELECT * FROM users LIMIT 15 OFFSET 15", sql)
		assert.Empty(t, params)
	})
}

func TestQueryBuilder_Complex(t *testing.T) {
	qb := NewQueryBuilder(nil)
	
	t.Run("complex query", func(t *testing.T) {
		sql, params, err := qb.Table("users").
			Select("users.id", "users.name", "profiles.bio").
			Join("profiles", "users.id = profiles.user_id").
			Where("users.status", "=", "active").
			Where("users.age", ">=", 18).
			OrWhere("users.role", "=", "admin").
			OrderBy("users.created_at", "DESC").
			Limit(50).
			ToSQL()
		
		require.NoError(t, err)
		expected := "SELECT users.id, users.name, profiles.bio FROM users INNER JOIN profiles ON users.id = profiles.user_id WHERE users.status = ? AND users.age >= ? OR users.role = ? ORDER BY users.created_at DESC LIMIT 50"
		assert.Equal(t, expected, sql)
		assert.Equal(t, []interface{}{"active", 18, "admin"}, params)
	})
}

func TestQueryBuilder_Errors(t *testing.T) {
	qb := NewQueryBuilder(nil)
	
	t.Run("missing table", func(t *testing.T) {
		_, _, err := qb.Where("id", "=", 1).ToSQL()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "table name is required")
	})
}

func TestQueryBuilder_InsertUpdateDelete(t *testing.T) {
	// Create a test database connection for real operations
	db := setupTestDB(t)
	defer db.Close()
	
	// Create a test table
	_, err := db.Exec(`CREATE TABLE test_operations (
		id INTEGER PRIMARY KEY,
		name TEXT,
		email TEXT,
		age INTEGER
	)`)
	require.NoError(t, err)
	
	qb := NewQueryBuilder(db)
	
	t.Run("insert data structure", func(t *testing.T) {
		data := map[string]interface{}{
			"name":  "John Doe",
			"email": "john@example.com",
			"age":   30,
		}
		
		result, err := qb.Table("test_operations").Insert(data)
		require.NoError(t, err)
		assert.NotNil(t, result)
		
		id, err := result.LastInsertId()
		require.NoError(t, err)
		assert.Greater(t, id, int64(0))
	})
	
	t.Run("update data structure", func(t *testing.T) {
		data := map[string]interface{}{
			"name": "Jane Doe",
			"age":  25,
		}
		
		result, err := qb.Table("test_operations").
			Where("id", "=", 1).
			Update(data)
		require.NoError(t, err)
		assert.NotNil(t, result)
		
		rowsAffected, err := result.RowsAffected()
		require.NoError(t, err)
		assert.Equal(t, int64(1), rowsAffected)
	})
	
	t.Run("delete structure", func(t *testing.T) {
		result, err := qb.Table("test_operations").
			Where("id", "=", 1).
			Delete()
		require.NoError(t, err)
		assert.NotNil(t, result)

		rowsAffected, err := result.RowsAffected()
		require.NoError(t, err)
		assert.Equal(t, int64(1), rowsAffected)
	})
}

// ============================================================================
// MODEL TESTS
// ============================================================================

func TestModel(t *testing.T) {
	t.Run("create model with defaults", func(t *testing.T) {
		model := NewModel("users")
		assert.Equal(t, "users", model.Table())
		assert.Equal(t, "id", model.PrimaryKey())
		assert.False(t, model.HasSoftDelete())
	})

	t.Run("model with custom primary key", func(t *testing.T) {
		model := NewModel("users").WithPrimaryKey("user_id")
		assert.Equal(t, "user_id", model.PrimaryKey())
	})

	t.Run("model with soft delete", func(t *testing.T) {
		model := NewModel("posts").WithSoftDelete()
		assert.True(t, model.HasSoftDelete())
	})

	t.Run("model query adds soft delete filter", func(t *testing.T) {
		model := NewModel("posts").WithSoftDelete()
		qb := model.Query(nil)
		sql, _, err := qb.ToSQL()
		require.NoError(t, err)
		assert.Contains(t, sql, "WHERE deleted_at IS NULL")
	})

	t.Run("model query without soft delete", func(t *testing.T) {
		model := NewModel("users")
		qb := model.Query(nil)
		sql, _, err := qb.ToSQL()
		require.NoError(t, err)
		assert.NotContains(t, sql, "deleted_at")
	})
}

// ============================================================================
// SCOPE TESTS
// ============================================================================

func TestScopes(t *testing.T) {
	t.Run("apply single scope", func(t *testing.T) {
		qb := NewQueryBuilder(nil)
		sql, params, err := qb.Table("users").Scope(Active).ToSQL()
		require.NoError(t, err)
		assert.Equal(t, "SELECT * FROM users WHERE active = ?", sql)
		assert.Equal(t, []interface{}{true}, params)
	})

	t.Run("apply multiple scopes", func(t *testing.T) {
		qb := NewQueryBuilder(nil)
		sql, _, err := qb.Table("users").Scope(Active, Recent).ToSQL()
		require.NoError(t, err)
		assert.Contains(t, sql, "WHERE active = ?")
		assert.Contains(t, sql, "ORDER BY created_at DESC")
	})

	t.Run("custom scope", func(t *testing.T) {
		customScope := func(qb *QueryBuilder) *QueryBuilder {
			return qb.Where("role", "=", "admin")
		}

		qb := NewQueryBuilder(nil)
		sql, params, err := qb.Table("users").Scope(customScope).ToSQL()
		require.NoError(t, err)
		assert.Equal(t, "SELECT * FROM users WHERE role = ?", sql)
		assert.Equal(t, []interface{}{"admin"}, params)
	})

	t.Run("published scope", func(t *testing.T) {
		qb := NewQueryBuilder(nil)
		sql, _, err := qb.Table("posts").Scope(Published).ToSQL()
		require.NoError(t, err)
		assert.Contains(t, sql, "published = ?")
		assert.Contains(t, sql, "published_at IS NOT NULL")
	})

	t.Run("draft scope", func(t *testing.T) {
		qb := NewQueryBuilder(nil)
		sql, params, err := qb.Table("posts").Scope(Draft).ToSQL()
		require.NoError(t, err)
		assert.Contains(t, sql, "published = ?")
		assert.Equal(t, []interface{}{false}, params)
	})
}

// ============================================================================
// SOFT DELETE TESTS
// ============================================================================

func TestSoftDelete(t *testing.T) {
	t.Run("only trashed adds where not null", func(t *testing.T) {
		qb := NewQueryBuilder(nil)
		sql, _, err := qb.Table("posts").OnlyTrashed().ToSQL()
		require.NoError(t, err)
		assert.Contains(t, sql, "WHERE deleted_at IS NOT NULL")
	})

	t.Run("with trashed flag is set", func(t *testing.T) {
		qb := NewQueryBuilder(nil)
		qb.Table("posts").WithTrashed()
		assert.True(t, qb.includeTrashed)
	})
}

// ============================================================================
// RELATION TESTS
// ============================================================================

func TestRelations(t *testing.T) {
	t.Run("has many generates correct query", func(t *testing.T) {
		qb := NewQueryBuilder(nil).Table("users")
		relatedQB := qb.HasMany("posts", "user_id", 1)
		sql, params, err := relatedQB.ToSQL()
		require.NoError(t, err)
		assert.Equal(t, "SELECT * FROM posts WHERE user_id = ?", sql)
		assert.Equal(t, []interface{}{1}, params)
	})

	t.Run("belongs to generates correct query", func(t *testing.T) {
		qb := NewQueryBuilder(nil).Table("posts")
		relatedQB := qb.BelongsTo("users", "user_id", 5)
		sql, params, err := relatedQB.ToSQL()
		require.NoError(t, err)
		assert.Equal(t, "SELECT * FROM users WHERE id = ?", sql)
		assert.Equal(t, []interface{}{5}, params)
	})

	t.Run("has many with additional conditions", func(t *testing.T) {
		qb := NewQueryBuilder(nil).Table("users")
		relatedQB := qb.HasMany("posts", "user_id", 1).
			Where("published", "=", true).
			OrderBy("created_at", "DESC")
		sql, params, err := relatedQB.ToSQL()
		require.NoError(t, err)
		assert.Contains(t, sql, "FROM posts")
		assert.Contains(t, sql, "user_id = ?")
		assert.Contains(t, sql, "published = ?")
		assert.Contains(t, sql, "ORDER BY created_at DESC")
		assert.Equal(t, []interface{}{1, true}, params)
	})
}

// ============================================================================
// SECURITY TESTS
// ============================================================================

func TestSecurityValidation(t *testing.T) {
	t.Run("reject invalid table name", func(t *testing.T) {
		qb := NewQueryBuilder(nil)
		_, _, err := qb.Table("users; DROP TABLE users;--").ToSQL()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid table name")
	})

	t.Run("reject invalid column name in where", func(t *testing.T) {
		qb := NewQueryBuilder(nil)
		_, _, err := qb.Table("users").Where("id; DROP TABLE users;--", "=", 1).ToSQL()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid column name")
	})

	t.Run("reject invalid operator", func(t *testing.T) {
		qb := NewQueryBuilder(nil)
		_, _, err := qb.Table("users").Where("id", "INVALID", 1).ToSQL()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid operator")
	})

	t.Run("accept valid operators", func(t *testing.T) {
		validOps := []string{"=", "!=", "<>", "<", ">", "<=", ">=", "LIKE", "NOT LIKE"}
		for _, op := range validOps {
			qb := NewQueryBuilder(nil)
			_, _, err := qb.Table("users").Where("id", op, 1).ToSQL()
			assert.NoError(t, err, "operator %s should be valid", op)
		}
	})

	t.Run("reject SQL injection in join condition", func(t *testing.T) {
		qb := NewQueryBuilder(nil)
		_, _, err := qb.Table("users").Join("posts", "users.id = posts.user_id; DROP TABLE users;--").ToSQL()
		assert.Error(t, err)
	})
}

// ============================================================================
// CHUNK TESTS
// ============================================================================

func TestChunk(t *testing.T) {
	t.Run("chunk size must be positive", func(t *testing.T) {
		qb := NewQueryBuilder(nil).Table("users")
		err := qb.Chunk(0, func(rows *sql.Rows) bool { return true })
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "chunk size must be positive")
	})

	t.Run("chunk size negative", func(t *testing.T) {
		qb := NewQueryBuilder(nil).Table("users")
		err := qb.Chunk(-1, func(rows *sql.Rows) bool { return true })
		assert.Error(t, err)
	})
}