package sqltx

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"sync"
)

type transactionState uint8

const (
	stateActive transactionState = iota
	stateCommitted
	stateRolledBack
	stateCommitFailed
)

// Tx wraps an sql.Tx with safe, idempotent lifecycle management.
type Tx struct {
	ctx    context.Context
	cancel context.CancelFunc
	tx     *sql.Tx

	mu       sync.Mutex
	state    transactionState
	finalErr error
}

// Begin starts a transaction, applies its setup functions, and returns it.
// The returned transaction should be closed with defer immediately after a
// successful call.
func Begin(ctx context.Context, db Beginner, options ...Option) (*Tx, error) {
	if err := validateBegin(ctx, db); err != nil {
		return nil, err
	}
	cfg, err := transactionConfig(options)
	if err != nil {
		return nil, err
	}

	txCtx, cancel := transactionContext(ctx, cfg)
	raw, err := beginSQLTx(txCtx, db, cfg)
	if err != nil {
		cancelContext(cancel)
		return nil, err
	}

	transaction := &Tx{
		ctx:    txCtx,
		cancel: cancel,
		tx:     raw,
		state:  stateActive,
	}
	if err := runSetups(options, transaction); err != nil {
		return nil, err
	}
	return transaction, nil
}

func validateBegin(ctx context.Context, db Beginner) error {
	if ctx == nil {
		return ErrNilContext
	}
	if isNilBeginner(db) {
		return ErrNilDB
	}
	return nil
}

func transactionConfig(options []Option) (config, error) {
	if len(options) == 0 {
		return config{}, nil
	}
	return applyOptions(options)
}

func transactionContext(ctx context.Context, cfg config) (context.Context, context.CancelFunc) {
	if cfg.hasBudget {
		return context.WithTimeout(ctx, cfg.budget)
	}
	return ctx, nil
}

func beginSQLTx(ctx context.Context, db Beginner, cfg config) (*sql.Tx, error) {
	var sqlOptions *sql.TxOptions
	if cfg.hasTxOptions {
		optionsCopy := cfg.txOptions
		sqlOptions = &optionsCopy
	}
	raw, err := db.BeginTx(ctx, sqlOptions)
	if err != nil {
		return nil, fmt.Errorf("sqltx: begin: %w", err)
	}
	if raw == nil {
		return nil, ErrNilSQLTx
	}
	return raw, nil
}

func runSetups(options []Option, transaction *Tx) error {
	setupIndex, err := runSetupKind(options, transaction, optionAdapterSetup, 0)
	if err != nil {
		return err
	}
	_, err = runSetupKind(options, transaction, optionSetup, setupIndex)
	return err
}

func runSetupKind(options []Option, transaction *Tx, kind optionKind, index int) (int, error) {
	for i := range options {
		if options[i].kind != kind {
			continue
		}
		index++
		if err := runSetup(transaction.ctx, transaction.tx, transaction, index, options[i].setup); err != nil {
			return index, err
		}
	}
	return index, nil
}

func runSetup(ctx context.Context, raw *sql.Tx, transaction *Tx, index int, setup SetupFunc) error {
	if err := setup(ctx, raw); err != nil {
		setupErr := fmt.Errorf("sqltx: setup %d: %w", index, err)
		if rollbackErr := transaction.Close(); rollbackErr != nil {
			return errors.Join(
				setupErr,
				fmt.Errorf("sqltx: rollback after setup: %w", rollbackErr),
			)
		}
		return setupErr
	}
	return nil
}

// Context returns the transaction lifetime context. It includes the budget,
// when one was configured.
func (tx *Tx) Context() context.Context {
	if tx == nil || tx.ctx == nil {
		return context.Background()
	}
	return tx.ctx
}

// Commit commits the transaction. A successful commit is idempotent. If a
// commit attempt fails, later calls return the same error.
func (tx *Tx) Commit() error {
	if tx == nil {
		return sql.ErrTxDone
	}

	tx.mu.Lock()
	defer tx.mu.Unlock()

	switch tx.state {
	case stateCommitted:
		return nil
	case stateRolledBack:
		return sql.ErrTxDone
	case stateCommitFailed:
		return tx.finalErr
	case stateActive:
		// Continue with the commit attempt.
	}

	err := tx.tx.Commit()
	if err != nil {
		err = normalizeCommitError(tx.ctx, err)
		tx.state = stateCommitFailed
		tx.finalErr = err
		cancelContext(tx.cancel)
		return err
	}

	tx.state = stateCommitted
	cancelContext(tx.cancel)
	return nil
}

// Rollback rolls back an active transaction. It is safe to call repeatedly
// and is a no-op after the transaction has otherwise been finalized.
func (tx *Tx) Rollback() error {
	return tx.Close()
}

// Close rolls back an active transaction and releases its context resources.
// It is idempotent and is safe to defer before calling Commit.
func (tx *Tx) Close() error {
	if tx == nil {
		return nil
	}

	tx.mu.Lock()
	defer tx.mu.Unlock()

	switch tx.state {
	case stateCommitted, stateCommitFailed:
		return nil
	case stateRolledBack:
		return tx.finalErr
	case stateActive:
		// Continue with the rollback attempt.
	}

	err := tx.tx.Rollback()
	if errors.Is(err, sql.ErrTxDone) {
		err = nil
	}
	tx.state = stateRolledBack
	tx.finalErr = err
	cancelContext(tx.cancel)
	return err
}

func normalizeCommitError(ctx context.Context, err error) error {
	// database/sql can report ErrTxDone when its context watcher won the
	// race to roll back. The context cause is more useful in that case.
	if errors.Is(err, sql.ErrTxDone) {
		if cause := context.Cause(ctx); cause != nil {
			return cause
		}
	}
	return err
}

func cancelContext(cancel context.CancelFunc) {
	if cancel != nil {
		cancel()
	}
}

func isNilBeginner(db Beginner) bool {
	if db == nil {
		return true
	}
	value := reflect.ValueOf(db)
	return value.Kind() == reflect.Pointer && value.IsNil()
}
