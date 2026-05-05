package mcp

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"knowshelf/internal/pkg/auth"
	"knowshelf/internal/segment"
	"knowshelf/internal/store"
)

func TestInstructionsMatchRegisteredQueryTool(t *testing.T) {
	for _, disallowed := range []string{"get chunk", "book content by id", "read content", "intent"} {
		if strings.Contains(instructions, disallowed) {
			t.Fatalf("instructions = %q, should not mention unregistered workflow %q", instructions, disallowed)
		}
	}
	for _, want := range []string{"query", "listAllBooks", "question", "bookID", "rewrittenQueries", "hypotheticalAnswer", "切片", "摘要", "元数据", "分数", "具体书名", "优先调用 listAllBooks", "不要编造 bookID", "向量检索更好匹配相关文档切片", "不参与 BM25"} {
		if !strings.Contains(instructions, want) {
			t.Fatalf("instructions = %q, want substring %q", instructions, want)
		}
	}
	for _, want := range []string{"用户提到一本书", "listAllBooks", "bookID", "不要编造 bookID", "hypotheticalAnswer", "向量检索更好匹配文档切片"} {
		if !strings.Contains(queryToolDescription, want) {
			t.Fatalf("query tool description = %q, want substring %q", queryToolDescription, want)
		}
	}
}

func TestQueryToolInputSchemaUsesMultiQueryContract(t *testing.T) {
	schema := queryToolInputSchema()
	for _, want := range []string{"question", "bookID", "rewrittenQueries", "hypotheticalAnswer"} {
		if _, ok := schema.Properties[want]; !ok {
			t.Fatalf("query schema missing %q: %#v", want, schema.Properties)
		}
	}
	if _, ok := schema.Properties["query"]; ok {
		t.Fatalf("query schema still exposes old query field: %#v", schema.Properties)
	}
	if _, ok := schema.Properties["intent"]; ok {
		t.Fatalf("query schema still exposes removed intent field: %#v", schema.Properties)
	}
	for _, removed := range []string{"candidateLimit", "minScore", "rerank"} {
		if _, ok := schema.Properties[removed]; ok {
			t.Fatalf("query schema still exposes config-only field %q: %#v", removed, schema.Properties)
		}
		if containsString(schema.PropertyOrder, removed) {
			t.Fatalf("query schema property order still contains config-only field %q: %#v", removed, schema.PropertyOrder)
		}
	}
	if !containsString(schema.Required, "question") {
		t.Fatalf("query schema required = %#v, want question", schema.Required)
	}
	rewrittenSchema := schema.Properties["rewrittenQueries"]
	if rewrittenSchema.MaxItems == nil || *rewrittenSchema.MaxItems != maxRewrittenQueries {
		t.Fatalf("rewrittenQueries maxItems = %v, want %d", rewrittenSchema.MaxItems, maxRewrittenQueries)
	}
	bookIDSchema := schema.Properties["bookID"]
	if bookIDSchema.Minimum == nil || *bookIDSchema.Minimum != 1 {
		t.Fatalf("bookID minimum = %v, want 1", bookIDSchema.Minimum)
	}
	hypotheticalSchema := schema.Properties["hypotheticalAnswer"]
	for _, want := range []string{"假设答案", "向量检索更好匹配相关文档切片", "只参与向量语义召回", "不参与 BM25"} {
		if !strings.Contains(hypotheticalSchema.Description, want) {
			t.Fatalf("hypotheticalAnswer description = %q, want substring %q", hypotheticalSchema.Description, want)
		}
	}
}

func TestQueryToolSearchOptionsUseRequestArgsOnly(t *testing.T) {
	bookID := int64(12)
	server := &Server{}

	opts := server.querySearchOptions(queryArgs{
		Question:           "孙悟空",
		RewrittenQueries:   []string{"美猴王"},
		HypotheticalAnswer: "花果山",
		BookID:             &bookID,
		Limit:              3,
	})

	if opts.BookID != bookID || opts.Question != "孙悟空" || opts.HypotheticalAnswer != "花果山" || opts.Limit != 3 {
		t.Fatalf("query options did not preserve query args: %#v", opts)
	}
	if len(opts.RewrittenQueries) != 1 || opts.RewrittenQueries[0] != "美猴王" {
		t.Fatalf("rewritten queries = %#v, want 美猴王", opts.RewrittenQueries)
	}
}

func TestListAllBooksToolInputSchemaRejectsArguments(t *testing.T) {
	schema := listAllBooksToolInputSchema()
	if schema.Type != "object" {
		t.Fatalf("listAllBooks schema type = %q, want object", schema.Type)
	}
	if len(schema.Properties) != 0 {
		t.Fatalf("listAllBooks schema properties = %#v, want none", schema.Properties)
	}
	if schema.AdditionalProperties == nil || schema.AdditionalProperties.Not == nil {
		t.Fatalf("listAllBooks schema should reject additional properties: %#v", schema.AdditionalProperties)
	}
}

func TestListAllBooksToolReturnsIDAndTitle(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	markdownDir := filepath.Join(tmp, "books")
	writeMCPTestFile(t, filepath.Join(markdownDir, "book.md"), "# Book\n\n正文。\n")
	db, err := store.Open(ctx, filepath.Join(tmp, "test.sqlite"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	defer db.Close()
	seg, err := segment.New([]string{"zh"})
	if err != nil {
		t.Fatalf("segment.New() error = %v", err)
	}
	imported, err := db.ImportMarkdown(ctx, seg, store.ImportOptions{Name: "book.md", MarkdownDir: markdownDir})
	if err != nil {
		t.Fatalf("ImportMarkdown() error = %v", err)
	}
	server := &Server{db: db}

	_, output, err := server.listAllBooksTool(ctx, nil, listAllBooksArgs{})
	if err != nil {
		t.Fatalf("listAllBooksTool() error = %v", err)
	}
	if len(output.Books) != 1 {
		t.Fatalf("books = %#v, want one", output.Books)
	}
	if output.Books[0].ID != imported.BookID || output.Books[0].Title != "Book" {
		t.Fatalf("book = %#v, want id %d title Book", output.Books[0], imported.BookID)
	}
}

func TestQueryToolRejectsNonPositiveBookID(t *testing.T) {
	bookID := int64(0)
	var logOutput bytes.Buffer
	server := &Server{
		logger: slog.New(slog.NewTextHandler(&logOutput, &slog.HandlerOptions{Level: slog.LevelInfo})),
	}

	_, _, err := server.queryTool(context.Background(), nil, queryArgs{
		Question:           "孙悟空",
		RewrittenQueries:   []string{"美猴王", "齐天大圣"},
		HypotheticalAnswer: "花果山",
		BookID:             &bookID,
		Limit:              3,
	})
	if err == nil || !strings.Contains(err.Error(), "bookID must be positive") {
		t.Fatalf("queryTool() error = %v, want bookID validation error", err)
	}
	for _, want := range []string{
		"mcp query tool request received",
		"question=孙悟空",
		"rewrittenQueries=",
		"美猴王",
		"齐天大圣",
		"hypotheticalAnswer=花果山",
		"bookID=0",
		"limit=3",
	} {
		if !strings.Contains(logOutput.String(), want) {
			t.Fatalf("queryTool() log = %q, want substring %q", logOutput.String(), want)
		}
	}
}

func TestListAllBooksRequiresReadScope(t *testing.T) {
	if !containsString(requiredToolScopes("listAllBooks"), auth.ScopeRead) {
		t.Fatalf("listAllBooks scopes = %#v, want read scope", requiredToolScopes("listAllBooks"))
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func writeMCPTestFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}
