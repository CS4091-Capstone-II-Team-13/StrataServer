package service

import (
	"context"
	"errors"
	"time"

	"strata.themrdt.org/dev/server/internal/model"
	"strata.themrdt.org/dev/server/internal/store/postgres"
)

var (
	ErrProjectNotFound = errors.New("project not found")
	ErrProjectExists   = errors.New("project name already exists")
)

type ProjectService struct {
	projects *postgres.ProjectStore
}

func NewProjectService(projects *postgres.ProjectStore) *ProjectService {
	return &ProjectService{projects: projects}
}

func (s *ProjectService) Create(ctx context.Context, name, description, userID string) (*model.Project, error) {
	now := time.Now()
	p := &model.Project{
		ID:          generateUUID(),
		Name:        name,
		Description: description,
		CreatedBy:   userID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := s.projects.Create(ctx, p); err != nil {
		return nil, ErrProjectExists
	}

	return p, nil
}

func (s *ProjectService) Get(ctx context.Context, id string) (*model.Project, error) {
	p, err := s.projects.GetByID(ctx, id)
	if err != nil {
		return nil, ErrProjectNotFound
	}
	return p, nil
}

func (s *ProjectService) List(ctx context.Context, userID string) ([]*model.Project, error) {
	return s.projects.ListByUser(ctx, userID)
}

func (s *ProjectService) Delete(ctx context.Context, id string) error {
	return s.projects.Delete(ctx, id)
}
