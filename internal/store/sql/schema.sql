PRAGMA foreign_keys = ON;
PRAGMA busy_timeout = 5000;
PRAGMA journal_mode = WAL;

CREATE TABLE IF NOT EXISTS books (
	id INTEGER PRIMARY KEY,
	source_path TEXT NOT NULL,
	title TEXT NOT NULL,
	created_at INTEGER NOT NULL,
	modified_at INTEGER NOT NULL,
	active INTEGER NOT NULL DEFAULT 1,
	UNIQUE(source_path)
);

CREATE TABLE IF NOT EXISTS parent_chunks (
	id INTEGER PRIMARY KEY,
	book_id INTEGER NOT NULL REFERENCES books(id) ON DELETE CASCADE,
	title TEXT NOT NULL,
	heading_path TEXT NOT NULL,
	text TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_parent_chunks_book ON parent_chunks(book_id);

CREATE TABLE IF NOT EXISTS chunks (
	id INTEGER PRIMARY KEY,
	book_id INTEGER NOT NULL REFERENCES books(id) ON DELETE CASCADE,
	parent_chunk_id INTEGER NOT NULL REFERENCES parent_chunks(id) ON DELETE CASCADE,
	title TEXT NOT NULL,
	heading_path TEXT NOT NULL,
	text TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_chunks_book ON chunks(book_id);
CREATE INDEX IF NOT EXISTS idx_chunks_parent ON chunks(parent_chunk_id);

CREATE TABLE IF NOT EXISTS embeddings (
	chunk_id INTEGER NOT NULL REFERENCES chunks(id) ON DELETE CASCADE,
	embedded_at INTEGER NOT NULL,
	PRIMARY KEY(chunk_id)
);

CREATE VIRTUAL TABLE IF NOT EXISTS chunks_fts USING fts5(
	title_bm25,
	heading_bm25,
	text_bm25,
	tokenize='unicode61'
);

CREATE VIRTUAL TABLE IF NOT EXISTS chunk_vectors USING vec0(
	embedding float[1024] distance_metric=cosine,
	book_id integer partition key
);
