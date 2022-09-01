package pgutil

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

func NewRestore(DsnUri string) *Restore {
	return &Restore{Options: PGDRestoreOpts, DsnUri: DsnUri, Schemas: []string{"public"}}
}

func (x *Restore) Exec(filename string, opts ExecOptions) Result {
	result := Result{}
	options := []string{x.DsnUri, fmt.Sprintf("--file=%s%s", x.Path, filename)}
	options = append(options, x.restoreOptions()...)
	result.FullCommand = strings.Join(options, " ")
	cmd := exec.Command(PSQL, options...)

	//cmd.Env = append(os.Environ(), x.EnvPassword)
	stderrIn, _ := cmd.StderrPipe()
	go func() {
		result.Output = streamExecOutput(stderrIn, opts)
	}()
	cmd.Start()
	err := cmd.Wait()
	if exitError, ok := err.(*exec.ExitError); ok {
		result.Error = &ResultError{Err: err, ExitCode: exitError.ExitCode(), CmdOutput: result.Output}
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
