# Query Builder

Tjo includes a fluent, SQL-injection-safe query builder for building database queries without writing raw SQL.

## Quick Start

```go
import "github.com/jimmitjoo/tjo/database"

// Create a query builder
qb := database.NewQueryBuilder(db)

// Basic query
rows, err := qb.Table("users").
    Where("active", "=", true).
    OrderBy("created_at", "DESC").
    Get()
```

---

## Basic Queries

### Select All Columns

```go
rows, err := qb.Table("users").Get()
// SELECT * FROM users
```

### Select Specific Columns

```go
rows, err := qb.Table("users").
    Select("id", "name", "email").
    Get()
// SELECT id, name, email FROM users
```

### Get First Row

```go
row := qb.Table("users").Where("id", "=", 1).First()
// SELECT * FROM users WHERE id = ? LIMIT 1
```

---

## Where Conditions

### Basic Where

```go
qb.Table("users").Where("status", "=", "active")
// SELECT * FROM users WHERE status = ?
```

### Multiple Conditions (AND)

```go
qb.Table("users").
    Where("status", "=", "active").
    Where("role", "=", "admin")
// SELECT * FROM users WHERE status = ? AND role = ?
```

### Or Conditions

```go
qb.Table("users").
    Where("role", "=", "admin").
    OrWhere("role", "=", "moderator")
// SELECT * FROM users WHERE role = ? OR role = ?
```

### Supported Operators

| Operator | Description |
|----------|-------------|
| `=` | Equal |
| `!=`, `<>` | Not equal |
| `<`, `>` | Less than, greater than |
| `<=`, `>=` | Less than or equal, greater than or equal |
| `LIKE` | Pattern matching |
| `NOT LIKE` | Negative pattern matching |
| `IN` | In a list of values |
| `NOT IN` | Not in a list of values |
| `BETWEEN` | Between two values |
| `IS NULL` | Is null |
| `IS NOT NULL` | Is not null |
| `REGEXP` | Regular expression (MySQL) |

### Where In

```go
qb.Table("users").WhereIn("id", []interface{}{1, 2, 3})
// SELECT * FROM users WHERE id IN (?, ?, ?)
```

### Where Between

```go
qb.Table("users").WhereBetween("age", 18, 65)
// SELECT * FROM users WHERE age BETWEEN ? AND ?
```

### Where Null / Not Null

```go
// Find users without email
qb.Table("users").WhereNull("email")
// SELECT * FROM users WHERE email IS NULL

// Find users with email
qb.Table("users").WhereNotNull("email")
// SELECT * FROM users WHERE email IS NOT NULL
```

### LIKE Queries

```go
qb.Table("users").Where("name", "LIKE", "John%")
// SELECT * FROM users WHERE name LIKE ?
```

---

## Joins

### Inner Join

```go
qb.Table("users").
    Join("profiles", "users.id = profiles.user_id")
// SELECT * FROM users INNER JOIN profiles ON users.id = profiles.user_id
```

### Left Join

```go
qb.Table("users").
    LeftJoin("orders", "users.id = orders.user_id")
// SELECT * FROM users LEFT JOIN orders ON users.id = orders.user_id
```

### Right Join

```go
qb.Table("posts").
    RightJoin("users", "posts.author_id = users.id")
// SELECT * FROM posts RIGHT JOIN users ON posts.author_id = users.id
```

### Multiple Joins

```go
qb.Table("users").
    Join("profiles", "users.id = profiles.user_id").
    LeftJoin("orders", "users.id = orders.user_id").
    Select("users.name", "profiles.bio", "orders.total")
// SELECT users.name, profiles.bio, orders.total FROM users
// INNER JOIN profiles ON users.id = profiles.user_id
// LEFT JOIN orders ON users.id = orders.user_id
```

---

## Ordering and Grouping

### Order By

```go
// Ascending (default)
qb.Table("users").OrderBy("name", "ASC")
// SELECT * FROM users ORDER BY name ASC

// Descending
qb.Table("users").OrderBy("created_at", "DESC")
// SELECT * FROM users ORDER BY created_at DESC

// Multiple columns
qb.Table("users").
    OrderBy("status", "ASC").
    OrderBy("name", "DESC")
// SELECT * FROM users ORDER BY status ASC, name DESC
```

### Group By

```go
qb.Table("orders").
    Select("user_id", "COUNT(*) AS order_count").
    GroupBy("user_id")
// SELECT user_id, COUNT(*) AS order_count FROM orders GROUP BY user_id
```

### Having

```go
qb.Table("orders").
    Select("user_id", "SUM(total) AS total_spent").
    GroupBy("user_id").
    Having("SUM(total)", ">", 1000)
// SELECT user_id, SUM(total) AS total_spent FROM orders
// GROUP BY user_id HAVING SUM(total) > ?
```

---

## Pagination

### Limit and Offset

```go
qb.Table("users").Limit(10).Offset(20)
// SELECT * FROM users LIMIT 10 OFFSET 20
```

### Paginate Helper

```go
// Get page 3 with 15 items per page
qb.Table("users").Paginate(3, 15)
// SELECT * FROM users LIMIT 15 OFFSET 30
```

---

## Aggregates

### Count

```go
count, err := qb.Table("users").Where("active", "=", true).Count()
// SELECT COUNT(*) FROM users WHERE active = ?
```

### Exists

```go
exists, err := qb.Table("users").Where("email", "=", email).Exists()
if exists {
    // User with this email already exists
}
```

---

## Insert, Update, Delete

### Insert

```go
result, err := qb.Table("users").Insert(map[string]interface{}{
    "name":  "John Doe",
    "email": "john@example.com",
    "age":   30,
})
// INSERT INTO users (name, email, age) VALUES (?, ?, ?)

id, _ := result.LastInsertId()
```

### Update

```go
result, err := qb.Table("users").
    Where("id", "=", 1).
    Update(map[string]interface{}{
        "name": "Jane Doe",
        "age":  31,
    })
// UPDATE users SET name = ?, age = ? WHERE id = ?

rowsAffected, _ := result.RowsAffected()
```

### Delete

```go
result, err := qb.Table("users").Where("id", "=", 1).Delete()
// DELETE FROM users WHERE id = ?
```

---

## Transactions

```go
err := qb.Transaction(func(tx *database.QueryBuilder) error {
    // All operations use the same transaction
    _, err := tx.Table("accounts").
        Where("id", "=", fromAccountID).
        Update(map[string]interface{}{
            "balance": fromBalance - amount,
        })
    if err != nil {
        return err // Rollback
    }

    _, err = tx.Table("accounts").
        Where("id", "=", toAccountID).
        Update(map[string]interface{}{
            "balance": toBalance + amount,
        })
    if err != nil {
        return err // Rollback
    }

    // Record transfer
    _, err = tx.Table("transfers").Insert(map[string]interface{}{
        "from_account": fromAccountID,
        "to_account":   toAccountID,
        "amount":       amount,
    })

    return err // Commit if nil, rollback otherwise
})
```

---

## Union Queries

```go
admins := qb.Table("users").Where("role", "=", "admin")
mods := database.NewQueryBuilder(db).Table("users").Where("role", "=", "moderator")

// UNION (removes duplicates)
rows, err := admins.Union(mods).Get()

// UNION ALL (keeps duplicates)
rows, err := admins.UnionAll(mods).Get()
```

---

## Soft Deletes

### Enable Soft Delete on Model

```go
userModel := database.NewModel("users").WithSoftDelete()

// Queries automatically exclude deleted records
rows, err := userModel.Query(db).Get()
// SELECT * FROM users WHERE deleted_at IS NULL
```

### Soft Delete a Record

```go
qb.Table("users").Where("id", "=", 1).SoftDelete()
// UPDATE users SET deleted_at = NOW() WHERE id = ?
```

### Restore a Soft-Deleted Record

```go
qb.Table("users").Where("id", "=", 1).Restore()
// UPDATE users SET deleted_at = NULL WHERE id = ?
```

### Include Soft-Deleted Records

```go
qb.Table("users").WithTrashed().Get()
// SELECT * FROM users (includes deleted records)
```

### Only Soft-Deleted Records

```go
qb.Table("users").OnlyTrashed().Get()
// SELECT * FROM users WHERE deleted_at IS NOT NULL
```

### Force Delete (Permanent)

```go
qb.Table("users").Where("id", "=", 1).ForceDelete()
// DELETE FROM users WHERE id = ?
```

---

## Query Scopes

Scopes are reusable query constraints.

### Define Scopes

```go
// Custom scope
func ActiveUsers(qb *database.QueryBuilder) *database.QueryBuilder {
    return qb.Where("active", "=", true)
}

func RecentlyCreated(qb *database.QueryBuilder) *database.QueryBuilder {
    return qb.Where("created_at", ">", time.Now().AddDate(0, 0, -7))
}
```

### Use Scopes

```go
rows, err := qb.Table("users").
    Scope(ActiveUsers, RecentlyCreated).
    Get()
// SELECT * FROM users WHERE active = ? AND created_at > ?
```

### Built-in Scopes

```go
// Active records
qb.Table("users").Scope(database.Active)
// WHERE active = true

// Recent first
qb.Table("posts").Scope(database.Recent)
// ORDER BY created_at DESC

// Oldest first
qb.Table("posts").Scope(database.Oldest)
// ORDER BY created_at ASC

// Published content
qb.Table("posts").Scope(database.Published)
// WHERE published = true AND published_at IS NOT NULL

// Draft content
qb.Table("posts").Scope(database.Draft)
// WHERE published = false
```

---

## Chunking Large Results

Process large result sets in memory-efficient chunks.

### Chunk by Offset

```go
err := qb.Table("users").Chunk(100, func(rows *sql.Rows) bool {
    for rows.Next() {
        var user User
        rows.Scan(&user.ID, &user.Name, &user.Email)
        // Process user
    }
    return true // Continue processing, return false to stop
})
```

### Chunk by ID

More efficient for large tables with indexed IDs:

```go
err := qb.Table("users").ChunkByID(100, "id", func(rows *sql.Rows) bool {
    // Process chunk
    return true
})
```

---

## Relationship Helpers

### Has Many

```go
// Get all posts for a user
posts := qb.HasMany("posts", "user_id", userID)
rows, err := posts.Get()
```

### Belongs To

```go
// Get the user for a post
user := qb.BelongsTo("users", "id", post.UserID)
row := user.First()
```

---

## Model Pattern

Use the Model pattern for cleaner code:

```go
// Define model
var UserModel = database.NewModel("users").
    WithPrimaryKey("id").
    WithSoftDelete()

// Find by ID
row := UserModel.Find(db, 1)

// Get all
rows, err := UserModel.All(db)

// Create
result, err := UserModel.Create(db, map[string]interface{}{
    "name":  "John",
    "email": "john@example.com",
})

// Update
result, err := UserModel.Update(db, 1, map[string]interface{}{
    "name": "Jane",
})

// Delete
result, err := UserModel.Delete(db, 1)

// Custom query with soft delete
rows, err := UserModel.Query(db).
    Where("role", "=", "admin").
    OrderBy("name", "ASC").
    Get()
// Automatically adds WHERE deleted_at IS NULL
```

---

## Raw Queries

When you need full SQL control:

```go
// Raw select
rows, err := qb.Raw(
    "SELECT * FROM users WHERE created_at > ? AND role IN (?, ?)",
    startDate, "admin", "moderator",
)

// Raw execute
result, err := qb.RawExec(
    "UPDATE users SET login_count = login_count + 1 WHERE id = ?",
    userID,
)
```

---

## Debug: View Generated SQL

```go
sql, params, err := qb.Table("users").
    Select("id", "name").
    Where("active", "=", true).
    OrderBy("name", "ASC").
    ToSQL()

fmt.Println("SQL:", sql)
// SQL: SELECT id, name FROM users WHERE active = ? ORDER BY name ASC

fmt.Println("Params:", params)
// Params: [true]
```

---

## Security Features

The query builder includes built-in SQL injection protection:

1. **Identifier Validation**: Table names, column names, and join conditions are validated against safe patterns
2. **Parameterized Queries**: All values are passed as parameters, never interpolated into SQL
3. **Operator Whitelist**: Only known-safe SQL operators are allowed

```go
// This is safe - values are parameterized
qb.Table("users").Where("name", "=", userInput)

// These will error - invalid identifiers
qb.Table("users; DROP TABLE users--")  // Error: invalid table name
qb.Where("name; --", "=", "test")       // Error: invalid column name
qb.Where("name", "EVIL", "test")        // Error: invalid operator
```

---

## Complete Example

```go
package main

import (
    "database/sql"
    "log"

    "github.com/jimmitjoo/tjo/database"
    _ "github.com/lib/pq"
)

var UserModel = database.NewModel("users").WithSoftDelete()

func main() {
    db, _ := sql.Open("postgres", "...")

    // Create user
    result, err := UserModel.Create(db, map[string]interface{}{
        "name":  "Alice",
        "email": "alice@example.com",
        "role":  "user",
    })
    if err != nil {
        log.Fatal(err)
    }

    userID, _ := result.LastInsertId()
    log.Printf("Created user %d", userID)

    // Query with conditions
    qb := database.NewQueryBuilder(db)
    rows, err := qb.Table("users").
        Select("id", "name", "email").
        Where("role", "=", "user").
        Where("active", "=", true).
        OrderBy("created_at", "DESC").
        Limit(10).
        Get()
    if err != nil {
        log.Fatal(err)
    }
    defer rows.Close()

    for rows.Next() {
        var id int
        var name, email string
        rows.Scan(&id, &name, &email)
        log.Printf("User: %d - %s (%s)", id, name, email)
    }

    // Transaction
    err = qb.Transaction(func(tx *database.QueryBuilder) error {
        // Deactivate user
        _, err := tx.Table("users").
            Where("id", "=", userID).
            Update(map[string]interface{}{"active": false})
        if err != nil {
            return err
        }

        // Log the action
        _, err = tx.Table("audit_log").Insert(map[string]interface{}{
            "user_id": userID,
            "action":  "deactivated",
        })
        return err
    })
    if err != nil {
        log.Fatal(err)
    }
}
```

---

## Best Practices

1. **Use Models for Table Definitions**: Define models for your tables to get soft delete and custom primary key support
2. **Use Scopes for Reusability**: Create scopes for common query patterns
3. **Use Transactions for Multi-Step Operations**: Wrap related operations in transactions
4. **Use Chunking for Large Data Sets**: Process large result sets in chunks to avoid memory issues
5. **Check Errors**: Always check for errors from ToSQL(), Get(), and other methods
6. **Use Parameterized Queries**: Never concatenate user input into SQL strings

