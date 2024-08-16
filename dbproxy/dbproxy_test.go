package dbproxy

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestE2E(t *testing.T) {

	// Context tracks how long we'll wait to find credential files
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr, err := New(ctx, Config{
		DBCredentialPath: testDSNURIPath,
		PGCredentialPath: filepath.Join(tempDir, "pgbouncer.ini"),
		PGBStartScript:   "scripts/mock-start-pgbouncer.sh",
		PGBReloadScript:  "scripts/mock-start-pgbouncer.sh",
		LocalAddr:        "0.0.0.0:5432",
	})

	if err != nil {
		t.Fatal(err)
	}

	if err := ctx.Err(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel = context.WithCancel(context.Background())
	go func() {
		if err := mgr.Start(ctx); err != nil {
			if !errors.Is(context.Canceled, err) {
				t.Fatal(err)
			}
		}
	}()
	// Let manager run for a bit, then cancel it
	time.Sleep(100 * time.Millisecond)
	cancel()
	if err := ctx.Err(); !errors.Is(context.Canceled, err) {
		t.Fatal(err)
	}

}
