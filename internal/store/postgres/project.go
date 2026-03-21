package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"strata.themrdt.org/dev/server/internal/model"
)

type ProjectStore struct {
	pool *pgxpool.Pool
}

func NewProjectStore(pool *pgxpool.Pool) *ProjectStore {
	return &ProjectStore{pool: pool}
}

func (s *ProjectStore) Create(ctx context.Context, p *model.Project) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO projects (id, name, description, created_by, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		p.ID, p.Name, p.Description, p.CreatedBy, p.CreatedAt, p.UpdatedAt,
	)
	return err
}

func (s *ProjectStore) GetByID(ctx context.Context, id string) (*model.Project, error) {
	p := &model.Project{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, name, description, created_by, created_at, updated_at
		 FROM projects WHERE id = $1`,
		id,
	).Scan(&p.ID, &p.Name, &p.Description, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return p, nil
}

func (s *ProjectStore) ListByUser(ctx context.Context, userID string) ([]*model.Project, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT p.id, p.name, p.description, p.created_by, p.created_at, p.updated_at
		 FROM projects p
		 WHERE p.created_by = $1
		 ORDER BY p.updated_at DESC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var projects []*model.Project
	for rows.Next() {
		p := &model.Project{}
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

func (s *ProjectStore) Delete(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM projects WHERE id = $1`, id)
	return err
}
