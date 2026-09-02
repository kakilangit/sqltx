// Package postgres provides PostgreSQL-specific transaction options.
package postgres

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/kakilangit/tx"
)

// WithTransactionTimeout sets PostgreSQL transaction_timeout locally for the
// transaction. transaction_timeout requires a PostgreSQL version that
// supports the setting.
func WithTransactionTimeout(duration time.Duration) tx.Option {
	return timeoutOption("transaction_timeout", duration)
}

// WithStatementTimeout sets PostgreSQL statement_timeout locally for the
// transaction.
func WithStatementTimeout(duration time.Duration) tx.Option {
	return timeoutOption("statement_timeout", duration)
}

// WithLockTimeout sets PostgreSQL lock_timeout locally for the transaction.
func WithLockTimeout(duration time.Duration) tx.Option {
	return timeoutOption("lock_timeout", duration)
}

func timeoutOption(name string, duration time.Duration) tx.Option {
	validate := func() error {
		if duration < 0 {
			return fmt.Errorf("postgres: %s must not be negative", name)
		}
		return nil
	}

	setup := func(ctx context.Context, dbtx tx.DBTX) error {
		value := formatDuration(duration)
		if _, err := dbtx.ExecContext(
			ctx,
			`SELECT set_config($1, $2, true)`,
			name,
			value,
		); err != nil {
			return fmt.Errorf("postgres: set %s: %w", name, err)
		}
		return nil
	}

	return tx.NewSetupOption(validate, setup)
}

func formatDuration(duration time.Duration) string {
	if duration == 0 {
		return "0"
	}

	milliseconds := duration / time.Millisecond
	if duration%time.Millisecond != 0 {
		milliseconds++
	}
	return strconv.FormatInt(int64(milliseconds), 10) + "ms"
}
