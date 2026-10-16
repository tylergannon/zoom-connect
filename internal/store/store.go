package store

import (
	"context"
	"embed"
	"fmt"
	"strings"

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
	b, err := schema.ReadFile("migrations/001_initial.sql")
	if err != nil {
		p.Close()
		return nil, err
	}
	for _, statement := range strings.Split(string(b), ";") {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		if _, err = p.Exec(ctx, statement); err != nil {
			p.Close()
			return nil, fmt.Errorf("apply schema: %w", err)
		}
	}
	return &Store{Pool: p, Q: db.New(p)}, nil
}

func (s *Store) Close() { s.Pool.Close() }
