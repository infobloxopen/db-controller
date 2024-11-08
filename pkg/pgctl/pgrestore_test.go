package pgctl

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/lib/pq"
)

func TestBackupSchemas(t *testing.T) {

	// Create a connection to the isolated test database.
	testDB, err := getDB(dropSchemaDBAdminDsn, nil)
	if err != nil {
		t.Fatalf("failed to connect to test DB: %v", err)
	}
	defer closeDB(logger, testDB)

	// FIXME: This unit tests needs to verify it works with
	// a user dsn like is used in the controller code.
	restore, err := NewRestore(dropSchemaDBAdminDsn, dropSchemaDBAdminDsn, "dropschemaadmin")
	if err != nil {
		t.Fatal(err)
	}

	t.Run("Backup public schema", func(t *testing.T) {
		// Verify the schemas were created.
		schemasBefore, err := listSchemas(testDB)
		if err != nil {
			t.Fatalf("failed to list schemas before drop: %v", err)
		}

		if len(schemasBefore) == 0 {
			t.Fatal("no schemas were created for testing")
		}

		if err = restore.backupSchema(context.TODO()); err != nil {
			t.Fatalf("drop schemas failed: %s", err)
		}

		// Verify that all non-system schemas were dropped.
		schemasAfter, err := listSchemas(testDB)
		if err != nil {
			t.Fatalf("failed to list schemas after drop: %v", err)
		}
		if len(schemasAfter) != 0 {
			t.Fatalf("expected no schemas after drop, but found: %v", schemasAfter)
		}

		if err := restore.recreateSchema(context.TODO()); err != nil {
			t.Fatal(err)
		}

		// Verify that the schemas were recreated.
		schemasAfterRecreate, err := listSchemas(testDB)
		if err != nil {
			t.Fatalf("failed to list schemas after recreate: %v", err)
		}
		if len(schemasAfterRecreate) != len(schemasBefore) {
			t.Fatalf("expected %d schemas after recreate, but found: %v", len(schemasBefore), schemasAfterRecreate)
		}

	})

	t.Run("Drop schemas when there are no schemas", func(t *testing.T) {
		t.Skip("this is not supported")
		// Ensure no schemas exist before the test.
		schemasBefore, err := listSchemas(testDB)
		if err != nil {
			t.Fatalf("failed to list schemas before drop: %v", err)
		}
		if len(schemasBefore) != 0 {
			t.Fatalf("expected no schemas, but found: %v", schemasBefore)
		}

		// Call DropSchemas to verify it handles the empty state correctly.

		if err := restore.backupSchema(context.TODO()); err != nil {
			t.Fatalf("drop schemas failed when no schemas existed: %s", err)
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
        AND schema_name NOT LIKE 'pg_temp_%'
		AND schema_name NOT LIKE 'public_migrate_failed%'
`)
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
