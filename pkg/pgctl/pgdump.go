package pgctl

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

var (
	PGDump              = "pg_dump"
	PGDumpOpts          = []string{}
	PGDumpDefaultFormat = "p"
)

type Results struct {
	Dump    Result
	Restore Result
}

type Result struct {
	Mine        string
	FileName    string
	Output      string
	Error       *ResultError
	FullCommand string
}

type ResultError struct {
	Err       error
	CmdOutput string
	ExitCode  int
}

type Dump struct {
	DsnUri   string
	Verbose  bool
	Path     string
	Format   *string
	Options  []string
	fileName string
}

func NewDump(DsnUri string) *Dump {
	return &Dump{Options: PGDumpOpts, DsnUri: DsnUri}
}

func (x *Dump) Exec(opts ExecOptions) Result {
	result := Result{Mine: "application/x-tar"}
	result.FileName = x.GetFileName()
	options := append(x.dumpOptions(), fmt.Sprintf(`-f%s%v`, x.Path, result.FileName))
	result.FullCommand = strings.Join(options, " ")
	cmd := exec.Command(PGDump, options...)
	// cmd.Env = append(os.Environ(), x.EnvPassword)
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
func (x *Dump) ResetOptions() {
	x.Options = []string{}
}

func (x *Dump) EnableVerbose() {
	x.Verbose = true
}

func (x *Dump) SetFileName(filename string) {
	x.fileName = filename
}

func (x *Dump) GetFileName() string {
	if x.fileName == "" {
		// Use default file name
		x.fileName = x.newFileName()
	}
	return x.fileName
}

func (x *Dump) SetupFormat(f string) {
	x.Format = &f
}

func (x *Dump) SetPath(path string) {
	x.Path = path
}

func (x *Dump) newFileName() string {
	return fmt.Sprintf(`%v_%v.sql`, "pub", time.Now().Unix())
}

func (x *Dump) dumpOptions() []string {
	options := x.Options
	options = append(options, x.DsnUri)

	if x.Format != nil {
		options = append(options, fmt.Sprintf(`-F%v`, *x.Format))
	} else {
		options = append(options, fmt.Sprintf(`-F%v`, PGDumpDefaultFormat))
	}
	if x.Verbose {
		options = append(options, "-v")
	}
	return options
}

func (x *Dump) SetOptions(o []string) {
	x.Options = o
}
func (x *Dump) GetOptions() []string {
	return x.Options
}

// modifyPgDumpInfo modifies the pg_dump file to comment out certain policy
// creation statements, add "IF NOT EXISTS" to schema creation, and remove the
// schema specification for the pg_cron extension.
// This method is OS-specific, adjusting the sed command for macOS or other systems.
func (x *Dump) modifyPgDumpInfo() error {
	// Build the full file path
	filePath := x.Path + x.fileName

	// Determine the OS-specific sed command. On macOS, the '-i' option requires an argument
	// for the backup suffix (can be empty).
	sedCmd := "sed"
	sedArg := "-i"
	if runtime.GOOS == "darwin" {
		sedArg = "-i ''"
	}

	// Comment out the create policy statements
	commentCmd := exec.Command(sedCmd, sedArg, "/^CREATE POLICY cron_job_/s/^/-- commented by dbc to avoid duplicate conflict during restore \\n--/", filePath)
	commentCmd.Stderr = os.Stderr
	commentCmd.Stdout = os.Stdout

	if err := commentCmd.Run(); err != nil {
		return fmt.Errorf("error running sed command to comment create policy: %w", err)
	}

	// Add if not exists to partman schema creation
	replaceCmd := exec.Command(sedCmd, sedArg, "s/CREATE SCHEMA partman;/CREATE SCHEMA IF NOT EXISTS partman;/", filePath)
	replaceCmd.Stderr = os.Stderr
	replaceCmd.Stdout = os.Stdout
	if err := replaceCmd.Run(); err != nil {
		return fmt.Errorf("error running sed command to add if not exists to partman schema creation: %w", err)
	}

	// Create pg_cron without specifying the schema
	replacePgCronCmd := exec.Command(sedCmd, sedArg, "s/CREATE EXTENSION IF NOT EXISTS pg_cron WITH SCHEMA public;/CREATE EXTENSION IF NOT EXISTS pg_cron;/", filePath)
	replacePgCronCmd.Stderr = os.Stderr
	replacePgCronCmd.Stdout = os.Stdout
	if err := replacePgCronCmd.Run(); err != nil {
		return fmt.Errorf("error running sed command to create pg_cron without specifying the schema: %w", err)
	}

	return nil
}
