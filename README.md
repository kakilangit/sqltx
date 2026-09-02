# tx

A small Go library that makes `database/sql` transactions easier and safer.

- automatic commit and rollback
- safe deferred cleanup
- transaction budgets and `sql.TxOptions`
- transaction setup hooks
- PostgreSQL timeout helpers

## Install

```sh
go get github.com/kakilangit/tx
```

## Usage

`Run` is the simplest way to use a transaction. It commits when the callback
returns `nil`, and rolls back on an error or panic.

```go
err := tx.Run(ctx, db, func(ctx context.Context, transaction *tx.Tx) error {
    _, err := transaction.ExecContext(ctx,
        `UPDATE accounts SET balance = balance - $1 WHERE id = $2`,
        amount,
        accountID,
    )
    return err
},
    tx.WithBudget(5*time.Second),
    tx.WithTxOptions(sql.TxOptions{
        Isolation: sql.LevelSerializable,
    }),
)
```

For manual control, defer `Close` right after `Begin`. It safely becomes a
no-op after commit.

```go
transaction, err := tx.Begin(ctx, db, tx.WithBudget(5*time.Second))
if err != nil {
    return err
}
defer transaction.Close()

if _, err := transaction.ExecContext(transaction.Context(), query, args...); err != nil {
    return err
}

return transaction.Commit()
```

Commit errors are returned to the caller and are never retried automatically.

## Setup

Use a setup hook to configure the transaction before it is returned:

```go
tx.WithSetup(func(ctx context.Context, dbtx tx.DBTX) error {
    _, err := dbtx.ExecContext(ctx, `SET LOCAL ROLE app_user`)
    return err
})
```

PostgreSQL timeout helpers are available in the `postgres` package:

```go
postgres.WithTransactionTimeout(4 * time.Second)
postgres.WithStatementTimeout(2 * time.Second)
postgres.WithLockTimeout(500 * time.Millisecond)
```

These settings only apply to the current transaction.

## Development

```sh
make tools
make check
make test
make benchmark
```

CI uses the same commands.
