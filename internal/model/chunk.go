package model

import "time"

type Chunk struct {
	Hash        string    `json:"hash"`
	SizeBytes   int64     `json:"size_bytes"`
	MinioBucket string    `json:"-"`
	MinioKey    string    `json:"-"`
	RefCount    int       `json:"-"`
	CreatedAt   time.Time `json:"created_at"`
}
