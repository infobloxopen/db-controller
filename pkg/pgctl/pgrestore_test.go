package pgctl

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/lib/pq"
)

func TestDropSchemas(t *testing.T) {
	// Create a connection to the isolated test database.
	testDB, err := getDB(dropSchemaDBAdminDsn, nil)
	if err != nil {
		t.Fatalf("failed to connect to test DB: %v", err)
	}
	defer closeDB(logger, testDB)

	restore := NewRestore(dropSchemaDBAdminDsn)

	t.Run("Drop existing schemas", func(t *testing.T) {
		setupTestSchemas(t, testDB)

		// Verify the schemas were created.
		schemasBefore, err := listSchemas(testDB)
		if err != nil {
			t.Fatalf("failed to list schemas before drop: %v", err)
		}
		if len(schemasBefore) == 0 {
			t.Fatal("no schemas were created for testing")
		}

		dropResult := restore.DropSchemas()
		if dropResult.Error != nil {
			t.Fatalf("drop schemas failed: %v", dropResult.Error.Err)
		}

		// Verify that all non-system schemas were dropped.
		schemasAfter, err := listSchemas(testDB)
		if err != nil {
			t.Fatalf("failed to list schemas after drop: %v", err)
		}
		if len(schemasAfter) != 0 {
			t.Fatalf("expected no schemas after drop, but found: %v", schemasAfter)
		}
	})

	t.Run("Drop schemas when there are no schemas", func(t *testing.T) {
		// Ensure no schemas exist before the test.
		schemasBefore, err := listSchemas(testDB)
		if err != nil {
			t.Fatalf("failed to list schemas before drop: %v", err)
		}
		if len(schemasBefore) != 0 {
			t.Fatalf("expected no schemas, but found: %v", schemasBefore)
		}

		// Call DropSchemas to verify it handles the empty state correctly.
		dropResult := restore.DropSchemas()
		if dropResult.Error != nil {
			t.Fatalf("drop schemas failed when no schemas existed: %v", dropResult.Error.Err)
		}
	})
}

// setupTestSchemas creates two test schemas and a table in each schema.
func setupTestSchemas(t *testing.T, db *sql.DB) {
	schemaNames := []string{"test_schema1", "test_schema2"}
	for _, schema := range schemaNames {
		_, err := db.Exec(fmt.Sprintf("CREATE SCHEMA %s", pq.QuoteIdentifier(schema)))
		if err != nil {
			t.Fatalf("failed to create schema %s: %v", schema, err)
		}

		_, err = db.Exec(fmt.Sprintf("CREATE TABLE %s.test_table (id INT)", pq.QuoteIdentifier(schema)))
		if err != nil {
			t.Fatalf("failed to create table in schema %s: %v", schema, err)
		}
	}
}

// listSchemas returns a list of all schemas in the database that are not system schemas.
func listSchemas(db *sql.DB) ([]string, error) {
	rows, err := db.Query(`
        SELECT schema_name
        FROM information_schema.schemata
        WHERE schema_name NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
        AND schema_name NOT LIKE 'pg_temp_%'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var schemas []string
	for rows.Next() {
		var schemaName string
		if err := rows.Scan(&schemaName); err != nil {
			return nil, err
		}
		schemas = append(schemas, schemaName)
	}

	return schemas, nil
}
