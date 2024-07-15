package dockerdb

import (
	"testing"

	_ "github.com/lib/pq"
)

func TestDB(t *testing.T) {
	db, _, cleanup := Run(Config{Database: "testdb"})
	defer cleanup()
	defer db.Close()
	_, err := db.Exec("CREATE TABLE test (id SERIAL PRIMARY KEY, name TEXT)")
	if err != nil {
		panic(err)
	}
}
