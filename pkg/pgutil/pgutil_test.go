package pgutil

import (
	"fmt"
	"testing"
)

func TestDumpRestore(t *testing.T) {

	dump := NewDump("postgres://bjeevan:example@localhost:5433/pub")

	dump.SetupFormat("p")
	dump.SetPath("/tmp/")

	dump.EnableVerbose()

	dump.SetOptions([]string{"--schema-only", "--no-publication", "--no-subscriptions"})
	dumpExec := dump.Exec(ExecOptions{StreamPrint: true})
	fmt.Println(dumpExec.FullCommand)

	if dumpExec.Error != nil {
		fmt.Println(dumpExec.Error.Err)
		fmt.Println(dumpExec.Output)

	} else {
		fmt.Println("Dump success")
		fmt.Println(dumpExec.Output)
	}

	restore := NewRestore(("postgres://bjeevan:example@localhost:5434/sub"))
	restore.EnableVerbose()

	restore.Path = dump.Path

	restoreExec := restore.Exec(dumpExec.File, ExecOptions{StreamPrint: true})
	fmt.Println(restoreExec.FullCommand)
	if restoreExec.Error != nil {
		fmt.Println(restoreExec.Error.Err)
		fmt.Println(restoreExec.Output)

	} else {
		fmt.Println("Restore success")
		fmt.Println(restoreExec.Output)

	}
}
