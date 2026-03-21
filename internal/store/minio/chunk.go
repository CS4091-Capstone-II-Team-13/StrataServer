package minio

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/minio/minio-go/v7"
)

type ChunkStore struct {
	client *minio.Client
	bucket string
}

func NewChunkStore(client *minio.Client, bucket string) *ChunkStore {
	return &ChunkStore{client: client, bucket: bucket}
}

// KeyFromHash returns the MinIO object key for a chunk hash.
// Uses 2-level prefix: ab/cd/abcdef1234...
func KeyFromHash(hash string) string {
	return fmt.Sprintf("%s/%s/%s", hash[:2], hash[2:4], hash)
}

// Put uploads chunk data to MinIO.
func (s *ChunkStore) Put(ctx context.Context, hash string, data []byte) error {
	key := KeyFromHash(hash)
	_, err := s.client.PutObject(ctx, s.bucket, key, bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{
		ContentType: "application/octet-stream",
	})
	return err
}

// Get retrieves chunk data from MinIO. Caller must close the returned ReadCloser.
func (s *ChunkStore) Get(ctx context.Context, hash string) (io.ReadCloser, int64, error) {
	key := KeyFromHash(hash)
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, 0, err
	}

	info, err := obj.Stat()
	if err != nil {
		obj.Close()
		return nil, 0, err
	}

	return obj, info.Size, nil
}

// Exists checks if a chunk exists in MinIO.
func (s *ChunkStore) Exists(ctx context.Context, hash string) (bool, error) {
	key := KeyFromHash(hash)
	_, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		errResp := minio.ToErrorResponse(err)
		if errResp.Code == "NoSuchKey" {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Delete removes a chunk from MinIO.
func (s *ChunkStore) Delete(ctx context.Context, hash string) error {
	key := KeyFromHash(hash)
	return s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
}
