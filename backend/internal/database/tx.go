package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/equiprent/backend/internal/db"
)

// Transactor runs a function inside a database transaction. Services depend
// on this interface; repositories pick the transaction up from the context
// via Conn, so the same repository code works in and out of transactions.
type Transactor interface {
	InTx(ctx context.Context, fn func(ctx context.Context) error) error
	// InTxAs is InTx plus the actor identity used by the audit and booking
	// history triggers (SET LOCAL app.actor_user_id / app.actor_role / app.reason).
	InTxAs(ctx context.Context, actor Actor, fn func(ctx context.Context) error) error
}

// Actor identifies who performs the changes inside a transaction.
type Actor struct {
	UserID    uuid.UUID
	Role      string
	Reason    string
	RequestID string
}

type txKey struct{}

// TxManager implements Transactor on a pgx pool.
type TxManager struct {
	pool       *pgxpool.Pool
	maxRetries int
}

// NewTxManager returns a Transactor that retries serialization failures and
// deadlocks up to 3 times (outermost transaction only).
func NewTxManager(pool *pgxpool.Pool) *TxManager { return &TxManager{pool: pool, maxRetries: 3} }

// InTx implements Transactor. Nested calls join the outer transaction.
func (m *TxManager) InTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return m.InTxAs(ctx, Actor{}, fn)
}

// InTxAs implements Transactor.
func (m *TxManager) InTxAs(ctx context.Context, actor Actor, fn func(ctx context.Context) error) error {
	if tx, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		if actor != (Actor{}) {
			if err := setActor(ctx, tx, actor); err != nil {
				return err
			}
		}
		return fn(ctx)
	}

	var err error
	for attempt := 0; attempt <= m.maxRetries; attempt++ {
		err = m.runOnce(ctx, actor, fn)
		if !IsRetryable(err) || ctx.Err() != nil {
			return err
		}
	}
	return err
}

func (m *TxManager) runOnce(ctx context.Context, actor Actor, fn func(ctx context.Context) error) (err error) {
	tx, err := m.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(context.WithoutCancel(ctx))
			panic(p)
		}
		if err != nil {
			_ = tx.Rollback(context.WithoutCancel(ctx))
		}
	}()

	if actor != (Actor{}) {
		if err = setActor(ctx, tx, actor); err != nil {
			return err
		}
	}
	if err = fn(context.WithValue(ctx, txKey{}, tx)); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

func setActor(ctx context.Context, tx pgx.Tx, a Actor) error {
	actorID := ""
	if a.UserID != uuid.Nil {
		actorID = a.UserID.String()
	}
	_, err := tx.Exec(ctx, `SELECT set_config('app.actor_user_id', $1, true), set_config('app.actor_role', $2, true),
	                               set_config('app.reason', $3, true), set_config('app.request_id', $4, true)`,
		actorID, a.Role, a.Reason, a.RequestID)
	if err != nil {
		return fmt.Errorf("set actor context: %w", err)
	}
	return nil
}

// Conn returns the transaction stored in ctx, or the pool.
func Conn(ctx context.Context, pool *pgxpool.Pool) db.DBTX {
	if tx, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return tx
	}
	return pool
}

// Queries returns sqlc queries bound to the transaction in ctx (or the pool).
func Queries(ctx context.Context, pool *pgxpool.Pool) *db.Queries { return db.New(Conn(ctx, pool)) }

// Detached returns ctx without its transaction: queries made with it commit
// on their own. Use it for work with outside effects (sending email) whose
// bookkeeping must survive a rollback of the surrounding transaction.
func Detached(ctx context.Context) context.Context { return context.WithValue(ctx, txKey{}, nil) }

// InTransaction reports whether ctx carries a transaction.
func InTransaction(ctx context.Context) bool {
	_, ok := ctx.Value(txKey{}).(pgx.Tx)
	return ok
}

// IsRetryable reports serialization failures and deadlocks, which are safe
// to retry from the start of the transaction.
func IsRetryable(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "40001" || pgErr.Code == "40P01"
	}
	return false
}

// NoopTransactor runs fn directly; for unit tests of services.
type NoopTransactor struct{}

func (NoopTransactor) InTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return fn(ctx)
}

func (NoopTransactor) InTxAs(ctx context.Context, _ Actor, fn func(ctx context.Context) error) error {
	return fn(ctx)
}

// WithoutTx returns ctx detached from any transaction it carries, so the next
// InTx call starts its own (used to commit or fail one item independently).
func WithoutTx(ctx context.Context) context.Context { return context.WithValue(ctx, txKey{}, nil) }
