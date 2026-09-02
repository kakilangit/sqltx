package tx

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var (
	ErrNilContext    = errors.New("tx: nil context")
	ErrNilDB         = errors.New("tx: nil database")
	ErrInvalidOption = errors.New("tx: invalid option")
	ErrNilCallback   = errors.New("tx: nil callback")
	ErrNilSetup      = errors.New("tx: nil setup function")
	ErrInvalidBudget = errors.New("tx: budget must be greater than zero")
	ErrNilSQLTx      = errors.New("tx: database returned a nil transaction")
)

// Beginner is implemented by *sql.DB and by database wrappers that can begin
// an sql transaction.
type Beginner interface {
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
}

// DBTX is the query surface available to transaction setup functions.
// It intentionally omits Commit and Rollback.
type DBTX interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
	PrepareContext(context.Context, string) (*sql.Stmt, error)
}

// SetupFunc configures a transaction after it begins and before it is exposed
// to the caller.
type SetupFunc func(context.Context, DBTX) error

type config struct {
	txOptions    sql.TxOptions
	hasTxOptions bool
	hasBudget    bool
	budget       time.Duration
}

type optionKind uint8

const (
	optionInvalid optionKind = iota
	optionTxOptions
	optionBudget
	optionSetup
	optionAdapterSetup
)

// Option configures a transaction. Its zero value is invalid; use one of the
// option constructors in this package or a database adapter package.
type Option struct {
	kind      optionKind
	txOptions sql.TxOptions
	budget    time.Duration
	validate  func() error
	setup     SetupFunc
}

func applyOptions(options []Option) (config, error) {
	cfg := config{}
	for i := range options {
		if err := applyOption(&cfg, &options[i]); err != nil {
			return config{}, fmt.Errorf("tx: option %d: %w", i+1, err)
		}
	}
	return cfg, nil
}

func applyOption(cfg *config, option *Option) error {
	switch option.kind {
	case optionTxOptions:
		cfg.txOptions = option.txOptions
		cfg.hasTxOptions = true
	case optionBudget:
		if option.budget <= 0 {
			return ErrInvalidBudget
		}
		cfg.hasBudget = true
		cfg.budget = option.budget
	case optionSetup, optionAdapterSetup:
		return validateSetupOption(option)
	case optionInvalid:
		return ErrInvalidOption
	default:
		return ErrInvalidOption
	}
	return nil
}

func validateSetupOption(option *Option) error {
	if option.validate != nil {
		if err := option.validate(); err != nil {
			return err
		}
	}
	if option.setup == nil {
		return ErrNilSetup
	}
	return nil
}

// WithTxOptions configures the options passed to database/sql BeginTx.
func WithTxOptions(options sql.TxOptions) Option {
	return Option{kind: optionTxOptions, txOptions: options}
}

// WithBudget limits the complete transaction lifetime, including begin,
// setup, work, and commit.
func WithBudget(duration time.Duration) Option {
	return Option{kind: optionBudget, budget: duration}
}

// WithSetup adds a setup function that runs inside the transaction. Setup
// functions run in option declaration order.
func WithSetup(fn SetupFunc) Option {
	return Option{kind: optionSetup, setup: fn}
}

// NewSetupOption creates an Option for database-specific adapter packages.
// Validation runs before the transaction begins. Adapter setup runs before
// setup functions added directly with WithSetup.
func NewSetupOption(validate func() error, setup SetupFunc) Option {
	return Option{
		kind:     optionAdapterSetup,
		validate: validate,
		setup:    setup,
	}
}
