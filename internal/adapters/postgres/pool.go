package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Pool struct{ pool *pgxpool.Pool }

func NewPool(ctx context.Context, databaseURL string) (*Pool, error) {
	p, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	if err := p.Ping(ctx); err != nil {
		p.Close()
		return nil, err
	}
	return &Pool{pool: p}, nil
}

func (p *Pool) Close() {
	if p != nil && p.pool != nil {
		p.pool.Close()
	}
}
func (p *Pool) Pool() *pgxpool.Pool { return p.pool }
