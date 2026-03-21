package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"

	"strata.themrdt.org/dev/server/internal/model"
	miniostore "strata.themrdt.org/dev/server/internal/store/minio"
	"strata.themrdt.org/dev/server/internal/store/postgres"
)

var (
	ErrChunkHashMismatch = errors.New("chunk hash does not match content")
	ErrChunkTooLarge     = errors.New("chunk exceeds maximum size")
	ErrChunkNotFound     = errors.New("chunk not found")
)

type ChunkService struct {
	chunks  *postgres.ChunkStore
	blobs   *miniostore.ChunkStore
	maxSize int64
}

func NewChunkService(chunks *postgres.ChunkStore, blobs *miniostore.ChunkStore, maxSize int64) *ChunkService {
	return &ChunkService{
		chunks:  chunks,
		blobs:   blobs,
		maxSize: maxSize,
	}
}

// CheckExisting returns the subset of hashes that already exist in the store.
func (s *ChunkService) CheckExisting(ctx context.Context, hashes []string) (existing, needed []string, err error) {
	existing, err = s.chunks.CheckExisting(ctx, hashes)
	if err != nil {
		return nil, nil, fmt.Errorf("check existing chunks: %w", err)
	}

	existSet := make(map[string]struct{}, len(existing))
	for _, h := range existing {
		existSet[h] = struct{}{}
	}

	needed = make([]string, 0, len(hashes)-len(existing))
	for _, h := range hashes {
		if _, ok := existSet[h]; !ok {
			needed = append(needed, h)
		}
	}

	return existing, needed, nil
}

// Upload verifies and stores a chunk. Returns the hash.
func (s *ChunkService) Upload(ctx context.Context, expectedHash string, data []byte) error {
	if int64(len(data)) > s.maxSize {
		return ErrChunkTooLarge
	}

	// Verify hash.
	h := sha256.Sum256(data)
	actualHash := hex.EncodeToString(h[:])
	if actualHash != expectedHash {
		return ErrChunkHashMismatch
	}

	// Upload to MinIO.
	if err := s.blobs.Put(ctx, actualHash, data); err != nil {
		return fmt.Errorf("store chunk blob: %w", err)
	}

	// Record in PostgreSQL.
	chunk := &model.Chunk{
		Hash:        actualHash,
		SizeBytes:   int64(len(data)),
		MinioBucket: "chunks",
		MinioKey:    miniostore.KeyFromHash(actualHash),
		RefCount:    0, // Incremented when committed.
		CreatedAt:   time.Now(),
	}
	if err := s.chunks.Insert(ctx, chunk); err != nil {
		return fmt.Errorf("record chunk metadata: %w", err)
	}

	return nil
}

// Download retrieves a chunk by hash. Caller must close the reader.
func (s *ChunkService) Download(ctx context.Context, hash string) (io.ReadCloser, int64, error) {
	// Verify chunk exists in metadata.
	_, err := s.chunks.GetByHash(ctx, hash)
	if err != nil {
		return nil, 0, ErrChunkNotFound
	}

	reader, size, err := s.blobs.Get(ctx, hash)
	if err != nil {
		return nil, 0, fmt.Errorf("retrieve chunk blob: %w", err)
	}

	return reader, size, nil
}
