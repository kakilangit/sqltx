package sqltx

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var testDriverID atomic.Uint64

type driverState struct {
	mu          sync.Mutex
	begins      int
	commits     int
	rollbacks   int
	executions  []string
	options     driver.TxOptions
	commitErr   error
	rollbackErr error
}

type recordingDriver struct {
	state *driverState
}

func (d *recordingDriver) Open(string) (driver.Conn, error) {
	return &recordingConn{state: d.state}, nil
}

type recordingConn struct {
	state *driverState
}

func (c *recordingConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare is not supported by the test driver")
}

func (c *recordingConn) Close() error { return nil }

func (c *recordingConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

func (c *recordingConn) BeginTx(_ context.Context, options driver.TxOptions) (driver.Tx, error) {
	c.state.mu.Lock()
	defer c.state.mu.Unlock()
	c.state.begins++
	c.state.options = options
	return &recordingTx{state: c.state}, nil
}

func (c *recordingConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	c.state.mu.Lock()
	defer c.state.mu.Unlock()
	c.state.executions = append(c.state.executions, query)
	return driver.RowsAffected(1), nil
}

type recordingTx struct {
	state *driverState
}

func (tx *recordingTx) Commit() error {
	tx.state.mu.Lock()
	defer tx.state.mu.Unlock()
	tx.state.commits++
	return tx.state.commitErr
}

func (tx *recordingTx) Rollback() error {
	tx.state.mu.Lock()
	defer tx.state.mu.Unlock()
	tx.state.rollbacks++
	return tx.state.rollbackErr
}

// Ensure database/sql does not try to prepare ExecContext calls.
var _ driver.ExecerContext = (*recordingConn)(nil)

func newRecordingDB(t testing.TB) (*sql.DB, *driverState) {
	t.Helper()

	state := &driverState{}
	name := fmt.Sprintf("tx-test-%d", testDriverID.Add(1))
	sql.Register(name, &recordingDriver{state: state})
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, state
}

func (state *driverState) counts() (begins, commits, rollbacks, executions int) {
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.begins, state.commits, state.rollbacks, len(state.executions)
}

func requireNoError(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestBeginCommitAndClose(t *testing.T) {
	db, state := newRecordingDB(t)

	transaction, err := Begin(
		context.Background(),
		db,
		WithTxOptions(sql.TxOptions{
			Isolation: sql.LevelSerializable,
			ReadOnly:  true,
		}),
		WithSetup(func(ctx context.Context, dbtx DBTX) error {
			_, err := dbtx.ExecContext(ctx, "SET LOCAL example = true")
			return err
		}),
	)
	requireNoError(t, err)

	_, err = transaction.ExecContext(transaction.Context(), "UPDATE example")
	requireNoError(t, err)
	requireNoError(t, transaction.Commit())
	requireNoError(t, transaction.Commit())
	requireNoError(t, transaction.Close())

	begins, commits, rollbacks, executions := state.counts()
	if begins != 1 || commits != 1 || rollbacks != 0 || executions != 2 {
		t.Fatalf("counts = (%d, %d, %d, %d), want (1, 1, 0, 2)", begins, commits, rollbacks, executions)
	}

	state.mu.Lock()
	options := state.options
	state.mu.Unlock()
	if options.Isolation != driver.IsolationLevel(sql.LevelSerializable) || !options.ReadOnly {
		t.Fatalf("BeginTx options = %+v", options)
	}
}

func TestCloseRollsBackOnce(t *testing.T) {
	db, state := newRecordingDB(t)
	transaction, err := Begin(context.Background(), db)
	if err != nil {
		t.Fatalf("Begin() error: %v", err)
	}

	if err := transaction.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}
	if err := transaction.Close(); err != nil {
		t.Fatalf("second Close() error: %v", err)
	}
	commitErr := transaction.Commit()
	if !errors.Is(commitErr, sql.ErrTxDone) {
		t.Fatalf("Commit() after Close() error = %v, want sql.ErrTxDone", commitErr)
	}

	begins, commits, rollbacks, _ := state.counts()
	if begins != 1 || commits != 0 || rollbacks != 1 {
		t.Fatalf("counts = (%d, %d, %d), want (1, 0, 1)", begins, commits, rollbacks)
	}
}

func TestRunCommitsSuccessfulCallback(t *testing.T) {
	db, state := newRecordingDB(t)

	err := Run(context.Background(), db, func(ctx context.Context, transaction *Tx) error {
		if ctx != transaction.Context() {
			t.Fatal("callback did not receive transaction context")
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("transaction budget did not add a deadline")
		}
		_, err := transaction.ExecContext(ctx, "INSERT example")
		return err
	}, WithBudget(time.Second))
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	begins, commits, rollbacks, executions := state.counts()
	if begins != 1 || commits != 1 || rollbacks != 0 || executions != 1 {
		t.Fatalf("counts = (%d, %d, %d, %d), want (1, 1, 0, 1)", begins, commits, rollbacks, executions)
	}
}

type runErrorTest struct {
	name        string
	rollbackErr error
	wantErrors  []error
}

func TestRunRollsBackCallbackErrors(t *testing.T) {
	callbackErr := errors.New("callback failed")
	rollbackErr := errors.New("rollback failed")
	tests := []runErrorTest{
		{
			name:       "callback error",
			wantErrors: []error{callbackErr},
		},
		{
			name:        "callback and rollback errors",
			rollbackErr: rollbackErr,
			wantErrors:  []error{callbackErr, rollbackErr},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testRunRollbackCallbackError(t, test, callbackErr)
		})
	}
}

func testRunRollbackCallbackError(t *testing.T, test runErrorTest, callbackErr error) {
	t.Helper()
	db, state := newRecordingDB(t)
	state.rollbackErr = test.rollbackErr

	err := Run(context.Background(), db, func(context.Context, *Tx) error {
		return callbackErr
	})
	for _, wantErr := range test.wantErrors {
		if !errors.Is(err, wantErr) {
			t.Fatalf("Run() error = %v, want %v", err, wantErr)
		}
	}

	_, commits, rollbacks, _ := state.counts()
	if commits != 0 || rollbacks != 1 {
		t.Fatalf("counts = commits %d, rollbacks %d; want 0, 1", commits, rollbacks)
	}
}

func TestRunRollsBackPanic(t *testing.T) {
	db, state := newRecordingDB(t)
	panicValue := errors.New("panic")

	func() {
		defer func() {
			recoveredErr, ok := recover().(error)
			if !ok || !errors.Is(recoveredErr, panicValue) {
				t.Fatalf("recovered = %v, want %v", recoveredErr, panicValue)
			}
		}()
		_ = Run(context.Background(), db, func(context.Context, *Tx) error {
			panic(panicValue)
		})
	}()

	_, commits, rollbacks, _ := state.counts()
	if commits != 0 || rollbacks != 1 {
		t.Fatalf("counts = commits %d, rollbacks %d; want 0, 1", commits, rollbacks)
	}
}

func TestSetupFailureRollsBack(t *testing.T) {
	db, state := newRecordingDB(t)
	setupErr := errors.New("setup failed")

	transaction, err := Begin(context.Background(), db, WithSetup(func(context.Context, DBTX) error {
		return setupErr
	}))
	if transaction != nil {
		t.Fatal("Begin() returned a transaction after setup failure")
	}
	if !errors.Is(err, setupErr) {
		t.Fatalf("Begin() error = %v, want setup error", err)
	}

	begins, commits, rollbacks, _ := state.counts()
	if begins != 1 || commits != 0 || rollbacks != 1 {
		t.Fatalf("counts = (%d, %d, %d), want (1, 0, 1)", begins, commits, rollbacks)
	}
}

func TestInvalidOptionsDoNotBegin(t *testing.T) {
	validationErr := errors.New("validation failed")
	tests := []struct {
		name    string
		option  Option
		wantErr error
	}{
		{name: "zero budget", option: WithBudget(0), wantErr: ErrInvalidBudget},
		{name: "negative budget", option: WithBudget(-time.Second), wantErr: ErrInvalidBudget},
		{name: "zero option", option: Option{}, wantErr: ErrInvalidOption},
		{name: "nil setup", option: WithSetup(nil), wantErr: ErrNilSetup},
		{
			name:    "nil adapter setup",
			option:  NewSetupOption(nil, nil),
			wantErr: ErrNilSetup,
		},
		{
			name: "adapter validation",
			option: NewSetupOption(func() error {
				return validationErr
			}, func(context.Context, DBTX) error {
				return nil
			}),
			wantErr: validationErr,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, state := newRecordingDB(t)
			transaction, err := Begin(context.Background(), db, test.option)
			if transaction != nil {
				t.Fatal("Begin() returned a transaction for an invalid option")
			}
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("Begin() error = %v, want %v", err, test.wantErr)
			}

			begins, _, _, _ := state.counts()
			if begins != 0 {
				t.Fatalf("begin count = %d, want 0", begins)
			}
		})
	}
}

func TestExpiredBudgetPreventsCommit(t *testing.T) {
	db, state := newRecordingDB(t)

	transaction, err := Begin(context.Background(), db, WithBudget(time.Millisecond))
	if err != nil {
		t.Fatalf("Begin() error: %v", err)
	}
	<-transaction.Context().Done()

	if err := transaction.Commit(); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Commit() error = %v, want context.DeadlineExceeded", err)
	}
	if err := transaction.Close(); err != nil {
		t.Fatalf("Close() after expiration error: %v", err)
	}

	deadline := time.Now().Add(time.Second)
	for {
		_, commits, rollbacks, _ := state.counts()
		if commits == 0 && rollbacks == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("counts = commits %d, rollbacks %d; want 0, 1", commits, rollbacks)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestCommitFailureIsStable(t *testing.T) {
	db, state := newRecordingDB(t)
	commitErr := errors.New("commit failed")
	state.commitErr = commitErr

	transaction, err := Begin(context.Background(), db)
	if err != nil {
		t.Fatalf("Begin() error: %v", err)
	}
	if err := transaction.Commit(); !errors.Is(err, commitErr) {
		t.Fatalf("Commit() error = %v, want commit error", err)
	}
	if err := transaction.Commit(); !errors.Is(err, commitErr) {
		t.Fatalf("second Commit() error = %v, want original commit error", err)
	}
	if err := transaction.Close(); err != nil {
		t.Fatalf("Close() after failed commit error: %v", err)
	}

	_, commits, rollbacks, _ := state.counts()
	if commits != 1 || rollbacks != 0 {
		t.Fatalf("counts = commits %d, rollbacks %d; want 1, 0", commits, rollbacks)
	}
}
