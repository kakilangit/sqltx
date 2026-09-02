package sqltx

import (
	"context"
	"errors"
	"fmt"
)

// Run executes fn in a transaction. A nil callback result causes a commit;
// any error or panic causes a rollback. Panics are propagated after cleanup.
func Run(
	ctx context.Context,
	db Beginner,
	fn func(context.Context, *Tx) error,
	options ...Option,
) (err error) {
	if fn == nil {
		return ErrNilCallback
	}

	transaction, err := Begin(ctx, db, options...)
	if err != nil {
		return err
	}

	defer func() {
		if rollbackErr := transaction.Close(); rollbackErr != nil {
			wrapped := fmt.Errorf("sqltx: rollback: %w", rollbackErr)
			if err == nil {
				err = wrapped
			} else {
				err = errors.Join(err, wrapped)
			}
		}
	}()

	// The transaction context is derived from ctx by Begin and additionally
	// carries the optional transaction budget.
	if err := fn(transaction.Context(), transaction); err != nil { //nolint:contextcheck
		return fmt.Errorf("sqltx: callback: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("sqltx: commit: %w", err)
	}
	return nil
}
