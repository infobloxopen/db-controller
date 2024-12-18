package pgctl

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/lib/pq"
)

var (
	PSQL           = "psql"
	PGDRestoreOpts = []string{}
)

type Restore struct {
	AdminDSN     string
	UserRole     string
	DsnUri       string
	Verbose      bool
	Options      []string
	Schemas      []string
	logger       logr.Logger
	databaseName string

	adminDB *sql.DB
	retries int
}

type RestoreOptions = func(x *Restore)

// NewRestore creates a new Restore instance with the provided configuration.
func NewRestore(adminDSN, userDSN, userRole string, options ...RestoreOptions) (*Restore, error) {
	u, err := url.Parse(adminDSN)
	if err != nil {
		return nil, err
	}

	u, err = url.Parse(userDSN)
	if err != nil {
		return nil, err
	}

	databaseName := strings.TrimPrefix(u.Path, "/")
	if databaseName == "" {
		return nil, fmt.Errorf("database name not found in user DSN")
	}

	r := &Restore{
		AdminDSN:     adminDSN,
		Options:      PGDRestoreOpts,
		DsnUri:       userDSN,
		Schemas:      []string{"public"},
		databaseName: databaseName,
		UserRole:     userRole,
		retries:      3,
	}

	r.adminDB, err = sql.Open("postgres", adminDSN)
	if err != nil {
		return nil, err
	}

	for _, option := range options {
		option(r)
	}

	return r, nil
}

func WithRestoreLogger(logger logr.Logger) RestoreOptions {
	return func(x *Restore) {
		x.logger = logger.WithName("pg_restore")
	}
}

// Close closes the database connection.
func (x *Restore) Close() error {
	return x.adminDB.Close()
}

// Exec runs the pg_restore command with the provided
// filename and options. It does this with pgctl cli because
// multiple statements are used and parsing those in Go would
// be error prone.
func (x *Restore) Exec(sqlPath string, opts ExecOptions) error {
	count := 0
	for {
		err := x.exec(sqlPath, opts)
		if err == nil {
			return nil
		}
		if count > x.retries {
			x.logger.Error(err, "restore database failed", "count", count)
			return err
		}
		count++
		x.logger.Info("restore failed, backing up schema and re-attempting", "count", count)
		if x.moveSchema(context.Background()) != nil {
			return fmt.Errorf("failed to move schema: %w", err)
		}
	}
}

func (x *Restore) exec(sqlPath string, opts ExecOptions) error {
	options := []string{
		"-vON_ERROR_STOP=ON",
		fmt.Sprintf("--file=%s", sqlPath),
	}
	options = append(options, x.restoreOptions()...)

	logCmd := PSQL + " " + strings.Join(append([]string{SanitizeDSN(x.DsnUri)}, options...), " ")

	x.logger.Info("restoring", "full_command", logCmd)

	args := append([]string{x.DsnUri}, options...)

	cmd := exec.Command(PSQL, args...)

	// Pipe to capture error output.
	stderrIn, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	var lastLine string
	go logStdouterr(stderrIn, x.logger, &lastLine)
	defer stderrIn.Close()

	err = cmd.Start()
	if err != nil {
		return err
	}

	err = cmd.Wait()
	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			return fmt.Errorf("unexpected error executing pgctl: %w", err)
		}
		// Probably ran into an issue with a pgctl line. See if we have a lastLine to parse
		errLine := parseLastLine(sqlPath, lastLine)
		x.logger.Error(exitErr, "pgctl error", "lastLine", errLine)
		if strings.Contains(errLine.Error(), "already exists") {
			return fmt.Errorf("%s", errLine)
		}
		return errors.Join(err, fmt.Errorf("%s", errLine))
	}

	return nil
}

func (x *Restore) ResetOptions() {
	x.Options = []string{}
}

func (x *Restore) SetSchemas(schemas []string) {
	x.Schemas = schemas
}

func (x *Restore) restoreOptions() []string {
	options := x.Options

	if x.Verbose {
		options = append(options, "-a")
	}

	return options
}

func (x *Restore) SetOptions(o []string) {
	x.Options = o
}
func (x *Restore) GetOptions() []string {
	return x.Options
}

// moveSchema will backup the existing public schema then
// create a new one owned by the user role.
func (x *Restore) moveSchema(ctx context.Context) error {
	if err := x.backupSchema(ctx); err != nil {
		return fmt.Errorf("failed to backup schema: %w", err)
	}

	if err := x.recreateSchema(ctx); err != nil {
		return fmt.Errorf("failed to recreate schemas: %w", err)
	}

	return nil
}

func (x *Restore) backupSchema(ctx context.Context) error {
	sql := fmt.Sprintf(`DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = 'public') THEN
        EXECUTE 'ALTER SCHEMA public RENAME TO public_migrate_failed_%s';
    END IF;
END $$;
`, time.Now().Format("20060102150405"))

	return x.sqlExec(ctx, "backup_schema", sql)
}

// DropSchemas drops all schemas except the system ones.
func (x *Restore) recreateSchema(ctx context.Context) error {
	if x.UserRole == "" {
		return fmt.Errorf("user role not found")
	}

	if err := x.sqlExec(context.TODO(), "create_schema", "CREATE SCHEMA public AUTHORIZATION "+pq.QuoteIdentifier(x.UserRole)); err != nil {
		return fmt.Errorf("failed to create public schema: %w", err)
	}
	// Re-apply standard public permissions
	return x.sqlExec(context.TODO(), "grant_schema", `GRANT USAGE ON SCHEMA public to PUBLIC;`)
}

func (x *Restore) sqlExec(ctx context.Context, action, query string, args ...any) error {

	x.logger.V(1).Info(action, "full_command", query)

	db, err := sql.Open("postgres", x.AdminDSN)
	if err != nil {
		return fmt.Errorf("failed to open database connection: %w", err)
	}
	defer db.Close()

	_, err = db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w query: %s", err, query)
	}

	return err
}
