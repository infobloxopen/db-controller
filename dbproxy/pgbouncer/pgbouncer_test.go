package pgbouncer

import (
	"os"
	"path"
	"strings"
	"testing"
)

func TestStart(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	for _, script := range []string{"start-pgbouncer.sh", "reload-pgbouncer.sh"} {

		script := path.Join(wd, "..", "scripts", script)

		out, err := run(script)
		if err == nil {
			t.Fatal("expected error")
		}

		if len(out) == 0 {
			t.Fatal("expected output")
		}

		if !strings.Contains(out, "pgbouncer: not found") {
			t.Fatalf("expected pgbouncer not found got:\n%s", out)
		}
	}
}
