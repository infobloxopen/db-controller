package pgctl

import (
	"fmt"
	"os/exec"
	"strings"
)

var (
	PSQL           = "psql"
	PGDRestoreOpts = []string{}
)

type Restore struct {
	DsnUri  string
	Verbose bool
	Path    string
	Options []string
	Schemas []string
}

// NewRestore creates a new Restore instance with the provided configuration.
func NewRestore(DsnUri string) *Restore {
	return &Restore{
		Options: PGDRestoreOpts,
		DsnUri:  DsnUri,
		Schemas: []string{"public"},
	}
}

// Exec runs the pg_restore command with the provided filename and options.
func (x *Restore) Exec(filename string, opts ExecOptions) Result {
	options := []string{
		x.DsnUri,
		"-vON_ERROR_STOP=ON",
		fmt.Sprintf("--file=%s%s", x.Path, filename),
	}
	options = append(options, x.restoreOptions()...)

	result := Result{
		FullCommand: strings.Join(options, " "),
	}

	cmd := exec.Command(PSQL, options...)

	// Pipe to capture error output.
	stderrIn, err := cmd.StderrPipe()
	if err != nil {
		result.Error = &ResultError{Err: err}
		return result
	}

	go func() {
		result.Output = streamExecOutput(stderrIn, opts)
	}()

	err = cmd.Start()
	if err != nil {
		result.Error = &ResultError{Err: err, CmdOutput: result.Output}
		return result
	}

	err = cmd.Wait()
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			result.Error = &ResultError{Err: exitError, ExitCode: exitError.ExitCode(), CmdOutput: result.Output}
			return result
		}

		result.Error = &ResultError{Err: err, CmdOutput: result.Output}
		return result
	}

	return result
}

func (x *Restore) ResetOptions() {
	x.Options = []string{}
}

func (x *Restore) EnableVerbose() {
	x.Verbose = true
}

func (x *Restore) SetPath(path string) {
	x.Path = path
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

// DropSchemas drops all schemas except the system ones.
func (x *Restore) DropSchemas() Result {
	dropSchemaSQL := `
        DO $$ DECLARE
            r RECORD;
        BEGIN
            FOR r IN (SELECT nspname FROM pg_namespace WHERE nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast') AND nspname !~ '^pg_temp_') LOOP
                EXECUTE 'DROP SCHEMA IF EXISTS ' || quote_ident(r.nspname) || ' CASCADE';
            END LOOP;
        END $$;
    `

	result := Result{}
	cmd := exec.Command(PSQL, x.DsnUri, "-c", dropSchemaSQL)

	// Pipe to capture error output.
	stderrIn, err := cmd.StderrPipe()
	if err != nil {
		result.Error = &ResultError{Err: err}
		return result
	}

	go func() {
		result.Output = streamExecOutput(stderrIn, ExecOptions{})
	}()

	if err := cmd.Start(); err != nil {
		result.Error = &ResultError{Err: err, CmdOutput: result.Output}
		return result
	}

	if err := cmd.Wait(); err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			result.Error = &ResultError{Err: exitError, ExitCode: exitError.ExitCode(), CmdOutput: result.Output}
			return result
		}

		result.Error = &ResultError{Err: err, CmdOutput: result.Output}
		return result
	}

	return result
}
