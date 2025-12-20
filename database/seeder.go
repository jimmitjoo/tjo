package database

import (
	"database/sql"
	"fmt"
	"log"
	"reflect"
	"strings"
)

// validateTableName checks if the table name is a valid SQL identifier
// to prevent SQL injection attacks
func validateTableName(tableName string) error {
	if tableName == "" {
		return fmt.Errorf("table name is required")
	}
	if !isValidIdentifier(tableName) {
		return fmt.Errorf("invalid table name: %q - must contain only letters, numbers, and underscores", tableName)
	}
	return nil
}

// Seeder interface defines the structure for database seeders
type Seeder interface {
	Run(db *sql.DB) error
}

// SeederRegistry manages all registered seeders
type SeederRegistry struct {
	seeders map[string]Seeder
}

// NewSeederRegistry creates a new seeder registry
func NewSeederRegistry() *SeederRegistry {
	return &SeederRegistry{
		seeders: make(map[string]Seeder),
	}
}

// Register adds a seeder to the registry
func (sr *SeederRegistry) Register(name string, seeder Seeder) {
	sr.seeders[name] = seeder
}

// RunAll executes all registered seeders
func (sr *SeederRegistry) RunAll(db *sql.DB) error {
	log.Println("Starting database seeding...")
	
	for name, seeder := range sr.seeders {
		log.Printf("Running seeder: %s", name)
		if err := seeder.Run(db); err != nil {
			return fmt.Errorf("seeder %s failed: %w", name, err)
		}
		log.Printf("Seeder %s completed successfully", name)
	}
	
	log.Println("Database seeding completed!")
	return nil
}

// Run executes a specific seeder by name
func (sr *SeederRegistry) Run(db *sql.DB, name string) error {
	seeder, exists := sr.seeders[name]
	if !exists {
		return fmt.Errorf("seeder %s not found", name)
	}
	
	log.Printf("Running seeder: %s", name)
	if err := seeder.Run(db); err != nil {
		return fmt.Errorf("seeder %s failed: %w", name, err)
	}
	
	log.Printf("Seeder %s completed successfully", name)
	return nil
}

// ListSeeders returns all registered seeder names
func (sr *SeederRegistry) ListSeeders() []string {
	var names []string
	for name := range sr.seeders {
		names = append(names, name)
	}
	return names
}

// BaseSeeder provides common functionality for seeders
type BaseSeeder struct {
	TableName string
}

// BulkInsert performs bulk insertion of data
func (bs *BaseSeeder) BulkInsert(db *sql.DB, data []map[string]interface{}) error {
	if len(data) == 0 {
		return nil
	}

	// Validate table name to prevent SQL injection
	if err := validateTableName(bs.TableName); err != nil {
		return err
	}
	
	// Get columns from first row and validate them
	var columns []string
	for col := range data[0] {
		if !isValidIdentifier(col) {
			return fmt.Errorf("invalid column name: %q", col)
		}
		columns = append(columns, col)
	}
	
	// Build INSERT query
	placeholders := strings.Repeat("?,", len(columns))
	placeholders = placeholders[:len(placeholders)-1] // Remove trailing comma
	
	query := fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s)",
		bs.TableName,
		strings.Join(columns, ", "),
		placeholders,
	)
	
	// Prepare statement for better performance
	stmt, err := db.Prepare(query)
	if err != nil {
		return fmt.Errorf("failed to prepare bulk insert statement: %w", err)
	}
	defer stmt.Close()
	
	// Insert each row
	for _, row := range data {
		var values []interface{}
		for _, col := range columns {
			values = append(values, row[col])
		}
		
		if _, err := stmt.Exec(values...); err != nil {
			return fmt.Errorf("failed to insert row: %w", err)
		}
	}
	
	log.Printf("Bulk inserted %d rows into %s", len(data), bs.TableName)
	return nil
}

// TruncateTable clears all data from a table
func (bs *BaseSeeder) TruncateTable(db *sql.DB) error {
	// Validate table name to prevent SQL injection
	if err := validateTableName(bs.TableName); err != nil {
		return err
	}

	// Use DELETE for better compatibility across databases
	query := fmt.Sprintf("DELETE FROM %s", bs.TableName)
	
	if _, err := db.Exec(query); err != nil {
		return fmt.Errorf("failed to truncate table %s: %w", bs.TableName, err)
	}
	
	log.Printf("Truncated table: %s", bs.TableName)
	return nil
}

// DropAndRecreateTable drops and recreates a table (requires table schema)
func (bs *BaseSeeder) DropAndRecreateTable(db *sql.DB, createSQL string) error {
	// Validate table name to prevent SQL injection
	if err := validateTableName(bs.TableName); err != nil {
		return err
	}

	// Drop table if exists
	dropQuery := fmt.Sprintf("DROP TABLE IF EXISTS %s", bs.TableName)
	if _, err := db.Exec(dropQuery); err != nil {
		return fmt.Errorf("failed to drop table %s: %w", bs.TableName, err)
	}
	
	// Create table
	if _, err := db.Exec(createSQL); err != nil {
		return fmt.Errorf("failed to create table %s: %w", bs.TableName, err)
	}
	
	log.Printf("Dropped and recreated table: %s", bs.TableName)
	return nil
}

// Factory provides a fluent interface for creating seed data
type Factory struct {
	data []map[string]interface{}
}

// NewFactory creates a new factory instance
func NewFactory() *Factory {
	return &Factory{
		data: make([]map[string]interface{}, 0),
	}
}

// Create adds records to the factory
func (f *Factory) Create(count int, template map[string]interface{}) *Factory {
	for i := 0; i < count; i++ {
		record := make(map[string]interface{})
		
		// Copy template data
		for key, value := range template {
			record[key] = f.processValue(value, i)
		}
		
		f.data = append(f.data, record)
	}
	
	return f
}

// CreateOne adds a single record to the factory
func (f *Factory) CreateOne(record map[string]interface{}) *Factory {
	processedRecord := make(map[string]interface{})
	
	for key, value := range record {
		processedRecord[key] = f.processValue(value, 0)
	}
	
	f.data = append(f.data, processedRecord)
	return f
}

// processValue handles special value types like functions
func (f *Factory) processValue(value interface{}, index int) interface{} {
	// Check if value is a function
	v := reflect.ValueOf(value)
	if v.Kind() == reflect.Func {
		// Call function and return result
		results := v.Call([]reflect.Value{reflect.ValueOf(index)})
		if len(results) > 0 {
			return results[0].Interface()
		}
	}
	
	return value
}

// GetData returns all created data
func (f *Factory) GetData() []map[string]interface{} {
	return f.data
}

// Insert inserts the factory data into database using BaseSeeder
func (f *Factory) Insert(db *sql.DB, tableName string) error {
	seeder := &BaseSeeder{TableName: tableName}
	return seeder.BulkInsert(db, f.data)
}

// Common faker functions for generating test data
type Faker struct{}

// NewFaker creates a new faker instance
func NewFaker() *Faker {
	return &Faker{}
}

// Name returns a function that generates names
func (fake *Faker) Name() func(int) interface{} {
	names := []string{
		"John Doe", "Jane Smith", "Bob Johnson", "Alice Brown", "Charlie Wilson",
		"Diana Davis", "Eve Miller", "Frank Garcia", "Grace Martinez", "Henry Anderson",
	}
	
	return func(index int) interface{} {
		return names[index%len(names)]
	}
}

// Email returns a function that generates emails
func (fake *Faker) Email() func(int) interface{} {
	domains := []string{"example.com", "test.org", "demo.net"}
	
	return func(index int) interface{} {
		domain := domains[index%len(domains)]
		return fmt.Sprintf("user%d@%s", index+1, domain)
	}
}

// RandomInt returns a function that generates random integers in range
func (fake *Faker) RandomInt(min, max int) func(int) interface{} {
	return func(index int) interface{} {
		return min + (index % (max - min + 1))
	}
}

// Sequence returns a function that generates sequential values
func (fake *Faker) Sequence(format string) func(int) interface{} {
	return func(index int) interface{} {
		return fmt.Sprintf(format, index+1)
	}
}

// Boolean returns a function that generates alternating boolean values
func (fake *Faker) Boolean() func(int) interface{} {
	return func(index int) interface{} {
		return index%2 == 0
	}
}

// Now returns current timestamp
func (fake *Faker) Now() interface{} {
	return "NOW()"
}

// Example seeder implementation
type UserSeeder struct {
	BaseSeeder
}

// NewUserSeeder creates a new user seeder
func NewUserSeeder() *UserSeeder {
	return &UserSeeder{
		BaseSeeder: BaseSeeder{TableName: "users"},
	}
}

// Run executes the user seeder
func (us *UserSeeder) Run(db *sql.DB) error {
	// Clear existing data
	if err := us.TruncateTable(db); err != nil {
		return err
	}
	
	// Create fake data using factory
	faker := NewFaker()
	factory := NewFactory()
	
	factory.Create(100, map[string]interface{}{
		"name":       faker.Name(),
		"email":      faker.Email(),
		"age":        faker.RandomInt(18, 80),
		"is_active":  faker.Boolean(),
		"created_at": faker.Now(),
		"updated_at": faker.Now(),
	})
	
	// Add some specific users
	factory.CreateOne(map[string]interface{}{
		"name":       "Admin User",
		"email":      "admin@example.com",
		"age":        35,
		"is_active":  true,
		"created_at": faker.Now(),
		"updated_at": faker.Now(),
	})
	
	// Insert all data
	return us.BulkInsert(db, factory.GetData())
}

// Utility functions

// RunSeeder runs a single seeder
func RunSeeder(db *sql.DB, seeder Seeder) error {
	return seeder.Run(db)
}

// CreateSeederRegistry creates and registers common seeders
func CreateSeederRegistry() *SeederRegistry {
	registry := NewSeederRegistry()
	
	// Register common seeders
	registry.Register("users", NewUserSeeder())
	
	return registry
}