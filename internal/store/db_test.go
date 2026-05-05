package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenCreatesEmbeddingsWithoutModelOrDimColumns(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()

	columns := embeddingColumns(t, db)
	if columns["model"] {
		t.Fatal("embeddings table has model column")
	}
	if columns["dim"] {
		t.Fatal("embeddings table has dim column")
	}
	assertNoEmbeddingModelIndex(t, db)
	assertTableMissing(t, db, "sections")
	assertTableMissing(t, db, "llm_cache")
	assertTablesWithoutAutoIncrement(t, db, "books", "parent_chunks", "chunks")
	assertColumnsPresent(t, db, "chunk_vectors", "book_id")
	assertColumnsPresent(t, db, "chunks", "parent_chunk_id")
	assertColumnsPresent(t, db, "parent_chunks", "book_id", "heading_path", "text")
	assertColumnsMissing(t, db, "parent_chunks", "char_start", "char_end", "line_start", "line_end")
	assertColumnsMissing(t, db, "chunks", "char_start", "char_end", "line_start", "line_end")
	assertColumnTypes(t, db, "books", map[string]string{
		"created_at":  "INTEGER",
		"modified_at": "INTEGER",
	})
	assertColumnTypes(t, db, "embeddings", map[string]string{
		"embedded_at": "INTEGER",
	})
}

func embeddingColumns(t *testing.T, db *DB) map[string]bool {
	t.Helper()
	rows, err := db.query(context.Background(), `PRAGMA table_info(embeddings)`)
	if err != nil {
		t.Fatalf("read embeddings columns: %v", err)
	}
	defer rows.Close()

	columns := map[string]bool{}
	for rows.Next() {
		var cid int
		var name string
		var dataType string
		var notNull int
		var defaultValue sql.NullString
		var primaryKey int
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatalf("scan embeddings column: %v", err)
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate embeddings columns: %v", err)
	}
	return columns
}

func assertNoEmbeddingModelIndex(t *testing.T, db *DB) {
	t.Helper()
	var name string
	err := db.queryRow(context.Background(), `
		SELECT name
		FROM sqlite_master
		WHERE type = 'index' AND name = 'idx_embeddings_model'
	`).Scan(&name)
	if err == nil {
		t.Fatal("idx_embeddings_model still exists")
	}
	if err != sql.ErrNoRows {
		t.Fatalf("read embeddings model index: %v", err)
	}
}

func assertTableMissing(t *testing.T, db *DB, table string) {
	t.Helper()
	var name string
	err := db.queryRow(context.Background(), `
		SELECT name
		FROM sqlite_master
		WHERE type = 'table' AND name = ?
	`, table).Scan(&name)
	if err == nil {
		t.Fatalf("%s table still exists", table)
	}
	if err != sql.ErrNoRows {
		t.Fatalf("read %s table: %v", table, err)
	}
}

func assertColumnsPresent(t *testing.T, db *DB, table string, names ...string) {
	t.Helper()
	columns := tableColumns(t, db, table)
	for _, name := range names {
		if !columns[name] {
			t.Fatalf("%s table missing column %s", table, name)
		}
	}
}

func assertColumnsMissing(t *testing.T, db *DB, table string, names ...string) {
	t.Helper()
	columns := tableColumns(t, db, table)
	for _, name := range names {
		if columns[name] {
			t.Fatalf("%s table still has column %s", table, name)
		}
	}
}

func assertColumnTypes(t *testing.T, db *DB, table string, want map[string]string) {
	t.Helper()
	columns := tableColumnTypes(t, db, table)
	for name, wantType := range want {
		gotType, ok := columns[name]
		if !ok {
			t.Fatalf("%s table missing column %s", table, name)
		}
		if !strings.EqualFold(gotType, wantType) {
			t.Fatalf("%s.%s type = %s, want %s", table, name, gotType, wantType)
		}
	}
}

func tableColumns(t *testing.T, db *DB, table string) map[string]bool {
	t.Helper()
	rows, err := db.query(context.Background(), `PRAGMA table_info(`+table+`)`)
	if err != nil {
		t.Fatalf("read %s columns: %v", table, err)
	}
	defer rows.Close()
	columns := map[string]bool{}
	for rows.Next() {
		var cid int
		var name string
		var dataType string
		var notNull int
		var defaultValue sql.NullString
		var primaryKey int
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatalf("scan %s column: %v", table, err)
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate %s columns: %v", table, err)
	}
	return columns
}

func tableColumnTypes(t *testing.T, db *DB, table string) map[string]string {
	t.Helper()
	rows, err := db.query(context.Background(), `PRAGMA table_info(`+table+`)`)
	if err != nil {
		t.Fatalf("read %s columns: %v", table, err)
	}
	defer rows.Close()
	columns := map[string]string{}
	for rows.Next() {
		var cid int
		var name string
		var dataType string
		var notNull int
		var defaultValue sql.NullString
		var primaryKey int
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatalf("scan %s column: %v", table, err)
		}
		columns[name] = dataType
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate %s columns: %v", table, err)
	}
	return columns
}

func assertTablesWithoutAutoIncrement(t *testing.T, db *DB, tables ...string) {
	t.Helper()
	for _, table := range tables {
		var schema string
		err := db.queryRow(context.Background(), `
			SELECT sql
			FROM sqlite_master
			WHERE type = 'table' AND name = ?
		`, table).Scan(&schema)
		if err != nil {
			t.Fatalf("read %s schema: %v", table, err)
		}
		if strings.Contains(strings.ToUpper(schema), "AUTOINCREMENT") {
			t.Fatalf("%s schema contains AUTOINCREMENT: %s", table, schema)
		}
	}
}
