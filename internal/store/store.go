package store

import (
	"context"
	"embed"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tylergannon/zoom-connect/internal/store/db"
)

//go:embed migrations/*.sql
var schema embed.FS

type Store struct {
	Pool *pgxpool.Pool
	Q    *db.Queries
}

func Open(ctx context.Context, dsn string) (*Store, error) {
	p, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err = p.Ping(ctx); err != nil {
		p.Close()
		return nil, err
	}
	// Both migrations are idempotent, so existing POC databases upgrade in place.
	files, err := schema.ReadDir("migrations")
	if err != nil {
		p.Close()
		return nil, err
	}
	for _, file := range files {
		b, err := schema.ReadFile("migrations/" + file.Name())
		if err == nil {
			_, err = p.Exec(ctx, string(b))
		}
		if err != nil {
			p.Close()
			return nil, fmt.Errorf("apply schema %s: %w", file.Name(), err)
		}
	}
	return &Store{Pool: p, Q: db.New(p)}, nil
}

func (s *Store) Close() { s.Pool.Close() }
