package sqlite

import (
	"context"
	"database/sql"

	"omakiten/internal/domain"
)

type transactionKey struct{}
type transactionScope struct {
	store  *Store
	tx     *sql.Tx
	events []domain.Event
}

type queryExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (s *Store) transaction(ctx context.Context) *transactionScope {
	scope, _ := ctx.Value(transactionKey{}).(*transactionScope)
	if scope != nil && scope.store == s {
		return scope
	}
	return nil
}

func (s *Store) query(ctx context.Context) queryExecutor {
	if scope := s.transaction(ctx); scope != nil {
		return scope.tx
	}
	return s.db
}

func (s *Store) beginTransaction(ctx context.Context) (*sql.Tx, error) {
	if scope := s.transaction(ctx); scope != nil {
		return scope.tx, nil
	}
	return s.db.BeginTx(ctx, nil)
}

func (s *Store) commitTransaction(ctx context.Context, tx *sql.Tx) error {
	if s.transaction(ctx) != nil {
		return nil
	}
	return tx.Commit()
}

func (s *Store) rollbackTransaction(ctx context.Context, tx *sql.Tx) error {
	if s.transaction(ctx) != nil {
		return nil
	}
	return tx.Rollback()
}

// WithinTransaction binds existing repository operations to one atomic unit.
// Event publication happens after its outer commit and is discarded on rollback.
func (s *Store) WithinTransaction(ctx context.Context, work func(context.Context) error) error {
	if s.transaction(ctx) != nil {
		return work(ctx)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	scope := &transactionScope{store: s, tx: tx}
	if err := work(context.WithValue(ctx, transactionKey{}, scope)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	for _, event := range scope.events {
		s.publishEvent(ctx, event)
	}
	return nil
}
