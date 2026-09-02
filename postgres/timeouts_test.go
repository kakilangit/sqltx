package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/kakilangit/sqltx"
)

type rejectingBeginner struct {
	called bool
}

func (db *rejectingBeginner) BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error) {
	db.called = true
	return nil, errors.New("unexpected BeginTx call")
}

func TestNegativeTimeoutIsRejectedBeforeBegin(t *testing.T) {
	tests := []struct {
		name   string
		option func(time.Duration) sqltx.Option
	}{
		{name: "transaction", option: WithTransactionTimeout},
		{name: "statement", option: WithStatementTimeout},
		{name: "lock", option: WithLockTimeout},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := &rejectingBeginner{}
			transaction, err := sqltx.Begin(
				context.Background(),
				db,
				test.option(-time.Millisecond),
			)
			if transaction != nil {
				t.Fatal("Begin() returned a transaction for a negative timeout")
			}
			if err == nil {
				t.Fatal("Begin() returned no error for a negative timeout")
			}
			if db.called {
				t.Fatal("BeginTx was called before timeout validation")
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		want     string
	}{
		{name: "disabled", duration: 0, want: "0"},
		{name: "milliseconds", duration: 250 * time.Millisecond, want: "250ms"},
		{name: "rounds positive values up", duration: 1500 * time.Microsecond, want: "2ms"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := formatDuration(test.duration); got != test.want {
				t.Fatalf("formatDuration(%s) = %q, want %q", test.duration, got, test.want)
			}
		})
	}
}
