package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"strata.themrdt.org/dev/server/internal/model"
)

type UserStore struct {
	pool *pgxpool.Pool
}

func NewUserStore(pool *pgxpool.Pool) *UserStore {
	return &UserStore{pool: pool}
}

func (s *UserStore) Create(ctx context.Context, user *model.User) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO users (id, username, email, password_hash, role, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		user.ID, user.Username, user.Email, user.PasswordHash, user.Role, user.CreatedAt, user.UpdatedAt,
	)
	return err
}

func (s *UserStore) GetByUsername(ctx context.Context, username string) (*model.User, error) {
	u := &model.User{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, username, email, password_hash, role, created_at, updated_at
		 FROM users WHERE username = $1`,
		username,
	).Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.Role, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (s *UserStore) GetByID(ctx context.Context, id string) (*model.User, error) {
	u := &model.User{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, username, email, password_hash, role, created_at, updated_at
		 FROM users WHERE id = $1`,
		id,
	).Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.Role, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return u, nil
}
