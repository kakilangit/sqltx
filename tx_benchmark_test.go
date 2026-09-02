package sqltx

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

func benchmarkCallback(context.Context, *Tx) error { return nil }
func benchmarkSetup(context.Context, DBTX) error   { return nil }

func BenchmarkTransactionLifecycle(b *testing.B) {
	db, _ := newRecordingDB(b)
	ctx := context.Background()

	b.Run("database_sql", func(b *testing.B) {
		benchmarkDatabaseSQL(b, db, ctx)
	})
	b.Run("database_sql_with_budget", func(b *testing.B) {
		benchmarkDatabaseSQLWithBudget(b, db, ctx)
	})
	b.Run("begin_commit", func(b *testing.B) {
		benchmarkBeginCommit(b, db, ctx)
	})
	b.Run("run", func(b *testing.B) {
		benchmarkRun(b, db, ctx)
	})
	b.Run("run_with_setup", func(b *testing.B) {
		benchmarkRunWithSetup(b, db, ctx)
	})
	b.Run("run_with_budget", func(b *testing.B) {
		benchmarkRunWithBudget(b, db, ctx)
	})
	b.Run("run_with_budget_inline", func(b *testing.B) {
		benchmarkRunWithInlineBudget(b, db, ctx)
	})
}

func benchmarkDatabaseSQL(b *testing.B, db *sql.DB, ctx context.Context) {
	b.ReportAllocs()
	for b.Loop() {
		transaction, err := db.BeginTx(ctx, nil)
		if err != nil {
			b.Fatal(err)
		}
		if err := transaction.Commit(); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkDatabaseSQLWithBudget(b *testing.B, db *sql.DB, ctx context.Context) {
	b.ReportAllocs()
	for b.Loop() {
		budgetCtx, cancel := context.WithTimeout(ctx, time.Second)
		transaction, err := db.BeginTx(budgetCtx, nil)
		if err != nil {
			cancel()
			b.Fatal(err)
		}
		if err := transaction.Commit(); err != nil {
			cancel()
			b.Fatal(err)
		}
		cancel()
	}
}

func benchmarkBeginCommit(b *testing.B, db *sql.DB, ctx context.Context) {
	b.ReportAllocs()
	for b.Loop() {
		transaction, err := Begin(ctx, db)
		if err != nil {
			b.Fatal(err)
		}
		if err := transaction.Commit(); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkRun(b *testing.B, db *sql.DB, ctx context.Context) {
	b.ReportAllocs()
	for b.Loop() {
		if err := Run(ctx, db, benchmarkCallback); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkRunWithSetup(b *testing.B, db *sql.DB, ctx context.Context) {
	b.ReportAllocs()
	setup := WithSetup(benchmarkSetup)
	b.ResetTimer()
	for b.Loop() {
		if err := Run(ctx, db, benchmarkCallback, setup); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkRunWithBudget(b *testing.B, db *sql.DB, ctx context.Context) {
	b.ReportAllocs()
	budget := WithBudget(time.Second)
	b.ResetTimer()
	for b.Loop() {
		if err := Run(ctx, db, benchmarkCallback, budget); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkRunWithInlineBudget(b *testing.B, db *sql.DB, ctx context.Context) {
	b.ReportAllocs()
	for b.Loop() {
		if err := Run(ctx, db, benchmarkCallback, WithBudget(time.Second)); err != nil {
			b.Fatal(err)
		}
	}
}
