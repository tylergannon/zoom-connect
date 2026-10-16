package store

import (
	"context"
	"crypto/x509"
	"embed"
	"fmt"
	"os"

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
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	if pem := os.Getenv("DATABASE_CA_CERT"); pem != "" && cfg.ConnConfig.TLSConfig != nil {
		roots, err := x509.SystemCertPool()
		if err != nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM([]byte(pem)) {
			return nil, fmt.Errorf("DATABASE_CA_CERT contains no valid certificates")
		}
		cfg.ConnConfig.TLSConfig.RootCAs = roots
	}
	p, err := pgxpool.NewWithConfig(ctx, cfg)
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
