package model

import "time"

type File struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	Path      string    `json:"path"`
	CreatedAt time.Time `json:"created_at"`
}

type FileVersion struct {
	ID          string    `json:"id"`
	FileID      string    `json:"file_id"`
	CommitID    string    `json:"commit_id"`
	TotalSize   int64     `json:"total_size"`
	ChunkCount  int       `json:"chunk_count"`
	ChunkHashes []string  `json:"chunk_hashes"`
	CreatedAt   time.Time `json:"created_at"`
}
