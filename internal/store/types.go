// Package store 包含 SQLite 持久化和基础召回操作。
package store

import "time"

type Book struct {
	ID         int64
	SourcePath string
	Title      string
	CreatedAt  time.Time
	ModifiedAt time.Time
	Active     bool
}

// ParentChunk 是检索命中后返回给模型的上下文单元。
type ParentChunk struct {
	ID          int64
	BookID      int64
	Title       string
	HeadingPath string
	Text        string
}

// Chunk 是从书中切分出来的 child 检索单元。
type Chunk struct {
	ID            int64
	BookID        int64
	ParentChunkID int64
	Title         string
	HeadingPath   string
	Text          string
}

// ImportOptions 控制 Markdown 导入。
type ImportOptions struct {
	Name        string
	MarkdownDir string
}

// ImportResult 汇总一次导入操作。
type ImportResult struct {
	BookID       int64
	Title        string
	ParentChunks int
	Chunks       int
	Indexed      bool
}

// Candidate 是融合前的基础召回结果。
type Candidate struct {
	Chunk         Chunk
	ParentChunk   ParentChunk
	BookID        int64
	BookTitle     string
	SourcePath    string
	Score         float64
	Source        string
	RetrievalType string
	Query         string
	Rank          int
}

// Status 描述索引状态。
type Status struct {
	Books         int
	Chunks        int
	Embeddings    int
	ChunksToEmbed int
}

// PendingChunk 是尚未生成 embedding 的 chunk。
type PendingChunk struct {
	ID          int64
	BookID      int64
	Title       string
	HeadingPath string
	Text        string
}

// VectorRow 是 chunk_vectors 中的一条向量索引记录。
type VectorRow struct {
	RowID        int64     `json:"rowid"`
	ChunkID      int64     `json:"chunk_id,omitempty"`
	BookID       int64     `json:"book_id,omitempty"`
	BookTitle    string    `json:"book_title,omitempty"`
	Title        string    `json:"title,omitempty"`
	HeadingPath  string    `json:"heading_path,omitempty"`
	Text         string    `json:"text,omitempty"`
	EmbeddingDim int       `json:"embedding_dim"`
	Embedding    []float32 `json:"embedding"`
}
