package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"knowshelf/internal/config"
	"knowshelf/internal/segment"
	"knowshelf/internal/store"
)

func TestImportBookCopiesMarkdownToLibrary(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	sourcePath := filepath.Join(tmp, "source", "b.md")
	markdownDir := filepath.Join(tmp, "library")
	content := "# 西游记\n\n孙悟空大闹天宫。\n"

	writeTestFile(t, sourcePath, content)
	db, err := store.Open(ctx, filepath.Join(tmp, "test.sqlite"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	defer db.Close()
	seg, err := segment.New([]string{"zh"})
	if err != nil {
		t.Fatalf("segment.New() error = %v", err)
	}
	app := &application{
		cfg:         &config.Config{},
		markdownDir: markdownDir,
		db:          db,
		segmenter:   seg,
	}

	if err := app.importBook(ctx, sourcePath); err != nil {
		t.Fatalf("importBook() error = %v", err)
	}
	assertFileContent(t, sourcePath, content)
	assertFileContent(t, filepath.Join(markdownDir, "b.md"), content)

	results, err := db.SearchBM25(ctx, seg, "孙悟空", 5, 0)
	if err != nil {
		t.Fatalf("SearchBM25() error = %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected search results")
	}
	wantSourcePath := filepath.Join(markdownDir, "b.md")
	if results[0].SourcePath != wantSourcePath {
		t.Fatalf("SourcePath = %q, want %q", results[0].SourcePath, wantSourcePath)
	}
}

func TestSearchCommandDoesNotExposeIntentFlag(t *testing.T) {
	configPath := ""
	cmd := newSearchCommand(&configPath, nil)

	if cmd.Flags().Lookup("intent") != nil {
		t.Fatal("search command still exposes removed intent flag")
	}
}

func TestShowVectorRowsWritesDefaultSingleRow(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	markdownDir := filepath.Join(tmp, "library")
	sourcePath := filepath.Join(markdownDir, "book.md")
	writeTestFile(t, sourcePath, "# Book\n\n向量测试内容。\n")
	db, err := store.Open(ctx, filepath.Join(tmp, "test.sqlite"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	defer db.Close()
	seg, err := segment.New([]string{"zh"})
	if err != nil {
		t.Fatalf("segment.New() error = %v", err)
	}
	if _, err := db.ImportMarkdown(ctx, seg, store.ImportOptions{Name: "book.md", MarkdownDir: markdownDir}); err != nil {
		t.Fatalf("ImportMarkdown() error = %v", err)
	}
	chunks, err := db.PendingEmbeddingChunks(ctx, 1)
	if err != nil {
		t.Fatalf("PendingEmbeddingChunks() error = %v", err)
	}
	vector := make([]float32, 1024)
	vector[0] = 0.125
	if err := db.SaveEmbedding(ctx, chunks[0].ID, vector); err != nil {
		t.Fatalf("SaveEmbedding() error = %v", err)
	}
	app := &application{db: db}
	var out bytes.Buffer

	if err := app.showVectorRows(ctx, 1, &out); err != nil {
		t.Fatalf("showVectorRows() error = %v", err)
	}
	var rows []store.VectorRow
	if err := json.Unmarshal(out.Bytes(), &rows); err != nil {
		t.Fatalf("unmarshal output: %v\n%s", err, out.String())
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if !strings.Contains(rows[0].Text, "向量测试内容") {
		t.Fatalf("Text = %q, want child chunk text", rows[0].Text)
	}
	if rows[0].RowID != chunks[0].ID || rows[0].EmbeddingDim != 1024 || rows[0].Embedding[0] != vector[0] {
		t.Fatalf("unexpected row: %#v", rows[0])
	}
}

func TestExportProjectorWritesVectorsAndMetadata(t *testing.T) {
	ctx := context.Background()
	db, chunkID := testDBWithEmbedding(t, ctx, "# Book\n\n第一行\t第二列。\n第二行。\n")
	defer db.Close()
	app := &application{db: db}
	outDir := filepath.Join(t.TempDir(), "projector")

	if err := app.exportProjector(ctx, outDir, 0, io.Discard); err != nil {
		t.Fatalf("exportProjector() error = %v", err)
	}

	vectors := readTestFile(t, filepath.Join(outDir, "vectors.tsv"))
	vectorLines := strings.Split(strings.TrimSpace(vectors), "\n")
	if len(vectorLines) != 1 {
		t.Fatalf("vectors.tsv line count = %d, want 1", len(vectorLines))
	}
	if cols := strings.Split(vectorLines[0], "\t"); len(cols) != 1024 {
		t.Fatalf("vectors.tsv column count = %d, want 1024", len(cols))
	}

	metadata := readTestFile(t, filepath.Join(outDir, "metadata.tsv"))
	metadataLines := strings.Split(strings.TrimSpace(metadata), "\n")
	if len(metadataLines) != 2 {
		t.Fatalf("metadata.tsv line count = %d, want 2: %q", len(metadataLines), metadata)
	}
	const wantHeader = "rowid\tchunk_id\tbook_id\tbook_title\ttitle\theading_path\ttext"
	if metadataLines[0] != wantHeader {
		t.Fatalf("metadata header = %q, want %q", metadataLines[0], wantHeader)
	}
	fields := strings.Split(metadataLines[1], "\t")
	if len(fields) != 7 {
		t.Fatalf("metadata field count = %d, want 7: %q", len(fields), metadataLines[1])
	}
	if fields[0] != strconv.FormatInt(chunkID, 10) || fields[1] != strconv.FormatInt(chunkID, 10) {
		t.Fatalf("metadata ids = rowid:%s chunk_id:%s, want %d", fields[0], fields[1], chunkID)
	}
	if strings.ContainsAny(fields[6], "\t\r\n") {
		t.Fatalf("metadata text contains TSV-breaking whitespace: %q", fields[6])
	}
	if !strings.Contains(fields[6], "第一行") || !strings.Contains(fields[6], "第二行") {
		t.Fatalf("metadata text = %q, want full child chunk text", fields[6])
	}
}

func TestExportProjectorRejectsNegativeLimit(t *testing.T) {
	ctx := context.Background()
	db, _ := testDBWithEmbedding(t, ctx, "# Book\n\n向量测试内容。\n")
	defer db.Close()
	app := &application{db: db}

	err := app.exportProjector(ctx, t.TempDir(), -1, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "limit must be non-negative") {
		t.Fatalf("exportProjector() error = %v, want negative limit error", err)
	}
}

func testDBWithEmbedding(t *testing.T, ctx context.Context, markdown string) (*store.DB, int64) {
	t.Helper()
	tmp := t.TempDir()
	markdownDir := filepath.Join(tmp, "library")
	sourcePath := filepath.Join(markdownDir, "book.md")
	writeTestFile(t, sourcePath, markdown)
	db, err := store.Open(ctx, filepath.Join(tmp, "test.sqlite"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	seg, err := segment.New([]string{"zh"})
	if err != nil {
		_ = db.Close()
		t.Fatalf("segment.New() error = %v", err)
	}
	if _, err := db.ImportMarkdown(ctx, seg, store.ImportOptions{Name: "book.md", MarkdownDir: markdownDir}); err != nil {
		_ = db.Close()
		t.Fatalf("ImportMarkdown() error = %v", err)
	}
	chunks, err := db.PendingEmbeddingChunks(ctx, 1)
	if err != nil {
		_ = db.Close()
		t.Fatalf("PendingEmbeddingChunks() error = %v", err)
	}
	if len(chunks) != 1 {
		_ = db.Close()
		t.Fatalf("pending chunks = %d, want 1", len(chunks))
	}
	vector := make([]float32, 1024)
	vector[0] = 0.125
	vector[1] = -0.5
	if err := db.SaveEmbedding(ctx, chunks[0].ID, vector); err != nil {
		_ = db.Close()
		t.Fatalf("SaveEmbedding() error = %v", err)
	}
	return db, chunks[0].ID
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	return string(got)
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	got := readTestFile(t, path)
	if got != want {
		t.Fatalf("ReadFile(%q) = %q, want %q", path, got, want)
	}
}
