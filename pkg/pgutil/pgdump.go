package pgutil

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/lib/pq"
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
	File        string
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
	result.File = x.GetFileName()
	options := append(x.dumpOptions(), fmt.Sprintf(`-f%s%v`, x.Path, result.File))
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
	fmt.Println(pq.ParseURL(x.DsnUri))
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
