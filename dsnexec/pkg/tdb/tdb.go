package tdb

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"sync"
	"testing"
)

var (
	defaultDriver *d
)

type d struct {
	testCases map[string]*testCase
	mu        sync.RWMutex
}

func init() {
	defaultDriver = &d{
		testCases: make(map[string]*testCase),
	}
	sql.Register("tdb", defaultDriver)
}

func (d *d) Open(name string) (driver.Conn, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	tc, ok := d.testCases[name]
	if !ok {
		return nil, fmt.Errorf("no test case for %s", name)
	}
	return tc, nil
}

type testCase struct {
	t         *testing.T
	ctx       context.Context
	execCalls []ExecArgs
}

type ExecArgs struct {
	Query string
	Args  []driver.Value
}

func RegisterTestCase(t *testing.T, ctx context.Context, name string) func() []ExecArgs {
	tc := &testCase{
		t:         t,
		execCalls: make([]ExecArgs, 0),
		ctx:       ctx,
	}
	defaultDriver.mu.Lock()
	defer defaultDriver.mu.Unlock()
	if _, ok := defaultDriver.testCases[name]; ok {
		t.Fatalf("test case %s already registered", name)
	}
	defaultDriver.testCases[name] = tc
	return tc.getExecCalls
}

func (tc *testCase) getExecCalls() []ExecArgs {
	execCalls := tc.execCalls
	tc.execCalls = make([]ExecArgs, 0)
	return execCalls
}

func (tc *testCase) Exec(query string, args []driver.Value) (driver.Result, error) {
	tc.t.Logf("exec: %s ; %v", query, args)
	tc.execCalls = append(tc.execCalls, ExecArgs{
		Query: query,
		Args:  args,
	})
	return &result{}, nil
}

type result struct{}

func (r *result) LastInsertId() (int64, error) {
	return 0, fmt.Errorf("unsupported LastInsertId in tdb")
}

func (r *result) RowsAffected() (int64, error) {
	return 0, fmt.Errorf("unsupported RowsAffected in tdb")
}

func (tc *testCase) Prepare(query string) (driver.Stmt, error) {
	return nil, fmt.Errorf("unsupported Prepare in tdb")
}

func (tc *testCase) Begin() (driver.Tx, error) {
	return nil, fmt.Errorf("unsupported Begin in tdb")
}

func (tc *testCase) Close() error {
	return nil
}
