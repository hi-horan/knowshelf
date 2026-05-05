package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"knowshelf/internal/config"
	"knowshelf/internal/search"
	"knowshelf/internal/segment"
	"knowshelf/internal/store"

	"github.com/google/jsonschema-go/jsonschema"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"go.opentelemetry.io/otel/codes"
)

const (
	serverName              = "knowshelf"
	serverVersion           = "0.1.0"
	maxRewrittenQueries     = 3
	instructions            = "Knowshelf 用于检索已导入的 Markdown 书籍切片。用户提到具体书名、某本书或想限定书籍范围时，优先调用 listAllBooks 查看 Knowshelf 里是否已导入；如果匹配到书籍，调用 query 时传对应 bookID，只在这本书内检索；如果没有匹配到，不要编造 bookID，直接全库检索。调用 query 时必须传 question，可选传 rewrittenQueries（调用方提供的改写问题，最多 3 条）和 hypotheticalAnswer（调用方提供的假设答案或预回答，用来让向量检索更好匹配相关文档切片；仅参与向量召回，不参与 BM25）。结果包含 id、摘要、元数据、分数和召回解释。"
	queryToolDescription    = "按 question 检索书籍切片。若用户提到一本书，先用 listAllBooks 确认是否已导入，再用 bookID 限定单本书；未匹配到书籍时不要编造 bookID。rewrittenQueries 可增强 BM25/向量召回，hypotheticalAnswer 是假设答案或预回答，用于让向量检索更好匹配文档切片。"
	listAllBooksDescription = "列出所有已导入且 active 的书籍 id 和 title。"
)

type Server struct {
	db      *store.DB
	search  *search.Service
	segment *segment.Segmenter
	config  *config.Config
	logger  *slog.Logger
}

type Option func(*Server) error

func WithLogger(logger *slog.Logger) Option {
	return func(s *Server) error {
		if logger != nil {
			s.logger = logger
		}
		return nil
	}
}

func NewServer(db *store.DB, searchService *search.Service, seg *segment.Segmenter, cfg *config.Config, opts ...Option) (*Server, error) {
	server := &Server{
		db:      db,
		search:  searchService,
		segment: seg,
		config:  cfg,
		logger:  slog.Default(),
	}
	for _, opt := range opts {
		if err := opt(server); err != nil {
			return nil, err
		}
	}
	return server, nil
}

func (s *Server) Handler() http.Handler {
	server := s.sdkServer()
	handler := mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server {
		return server
	}, &mcpsdk.StreamableHTTPOptions{
		JSONResponse:   s.config.MCP.JSONResponse,
		Stateless:      s.config.MCP.Stateless,
		SessionTimeout: time.Duration(s.config.MCP.SessionTimeoutMS) * time.Millisecond,
		Logger:         s.log(),
	})
	return s.corsHTTPMiddleware(s.authHTTPMiddleware(handler))
}

func (s *Server) sdkServer() *mcpsdk.Server {
	server := mcpsdk.NewServer(&mcpsdk.Implementation{
		Name:    serverName,
		Version: serverVersion,
		Icons:   serverIcons(),
	}, &mcpsdk.ServerOptions{
		Instructions: instructions,
		Capabilities: &mcpsdk.ServerCapabilities{
			Tools: &mcpsdk.ToolCapabilities{},
		},
	})
	server.AddReceivingMiddleware(s.loggingMiddleware(), s.scopeMiddleware())
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "query",
		Description: queryToolDescription,
		InputSchema: queryToolInputSchema(),
	}, s.queryTool)
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "listAllBooks",
		Description: listAllBooksDescription,
		InputSchema: listAllBooksToolInputSchema(),
	}, s.listAllBooksTool)
	return server
}

func (s *Server) queryTool(ctx context.Context, _ *mcpsdk.CallToolRequest, args queryArgs) (*mcpsdk.CallToolResult, queryOutput, error) {
	ctx, span := startToolSpan(ctx, "query")
	defer span.End()

	s.logQueryToolRequest(ctx, args)

	if args.BookID != nil {
		if *args.BookID <= 0 {
			err := fmt.Errorf("bookID must be positive")
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return nil, queryOutput{}, err
		}
	}
	opts := s.querySearchOptions(args)
	results, err := s.search.Search(ctx, opts)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		s.log().LogAttrs(ctx, slog.LevelError, "mcp tool failed",
			slog.String("tool", "query"),
			slog.String("error", err.Error()),
			slog.Bool("has_question", args.Question != ""))
		return nil, queryOutput{}, err
	}
	return textResult(fmt.Sprintf("找到 %d 条结果", len(results))), queryOutput{Results: results}, nil
}

func (s *Server) logQueryToolRequest(ctx context.Context, args queryArgs) {
	attrs := []slog.Attr{
		slog.String("tool", "query"),
		slog.String("question", args.Question),
		slog.Any("rewrittenQueries", args.RewrittenQueries),
		slog.String("hypotheticalAnswer", args.HypotheticalAnswer),
		slog.Int("limit", args.Limit),
	}
	if args.BookID != nil {
		attrs = append(attrs, slog.Int64("bookID", *args.BookID))
	} else {
		attrs = append(attrs, slog.String("bookID", ""))
	}
	s.log().LogAttrs(ctx, slog.LevelInfo, "mcp query tool request received", attrs...)
}

func (s *Server) querySearchOptions(args queryArgs) search.Options {
	opts := search.Options{
		Question:           args.Question,
		RewrittenQueries:   args.RewrittenQueries,
		HypotheticalAnswer: args.HypotheticalAnswer,
		Limit:              args.Limit,
	}
	if args.BookID != nil {
		opts.BookID = *args.BookID
	}
	return opts
}

func (s *Server) listAllBooksTool(ctx context.Context, _ *mcpsdk.CallToolRequest, _ listAllBooksArgs) (*mcpsdk.CallToolResult, listAllBooksOutput, error) {
	ctx, span := startToolSpan(ctx, "listAllBooks")
	defer span.End()

	books, err := s.db.ListBooks(ctx)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		s.log().LogAttrs(ctx, slog.LevelError, "mcp tool failed",
			slog.String("tool", "listAllBooks"),
			slog.String("error", err.Error()))
		return nil, listAllBooksOutput{}, err
	}
	out := listAllBooksOutput{Books: make([]bookListItemOutput, 0, len(books))}
	for _, book := range books {
		out.Books = append(out.Books, bookListItemOutput{
			ID:    book.ID,
			Title: book.Title,
		})
	}
	return textResult(fmt.Sprintf("找到 %d 本书", len(out.Books))), out, nil
}

func textResult(text string) *mcpsdk.CallToolResult {
	return &mcpsdk.CallToolResult{
		Content: []mcpsdk.Content{
			&mcpsdk.TextContent{Text: text},
		},
	}
}

func queryToolInputSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		Type: "object",
		Properties: map[string]*jsonschema.Schema{
			"question": {
				Type:        "string",
				MinLength:   jsonschema.Ptr(1),
				Description: "用户原始问题，必填。服务端会 trim，trim 后不能为空。",
			},
			"rewrittenQueries": {
				Type:        "array",
				MaxItems:    jsonschema.Ptr(maxRewrittenQueries),
				Description: "调用方提供的改写问题，最多 3 条。每条会 trim，空字符串和重复项会被忽略；每条同时参与 BM25 和向量召回。",
				Items: &jsonschema.Schema{
					Type: "string",
				},
			},
			"hypotheticalAnswer": {
				Type:        "string",
				Description: "调用方提供的假设答案或预回答，可选；用于让向量检索更好匹配相关文档切片，只参与向量语义召回，不参与 BM25。",
			},
			"bookID": {
				Type:        "integer",
				Minimum:     jsonschema.Ptr(1.0),
				Description: "可选书籍 id；传入后只在该书内检索，未传则搜索全部书籍。可先调用 listAllBooks 获取。",
			},
			"limit": {
				Type:        "integer",
				Description: "最终搜索结果的最大数量。",
			},
		},
		Required:             []string{"question"},
		AdditionalProperties: &jsonschema.Schema{Not: &jsonschema.Schema{}},
		PropertyOrder: []string{
			"question",
			"rewrittenQueries",
			"hypotheticalAnswer",
			"bookID",
			"limit",
		},
	}
}

type queryArgs struct {
	Question           string   `json:"question"`
	RewrittenQueries   []string `json:"rewrittenQueries,omitempty"`
	HypotheticalAnswer string   `json:"hypotheticalAnswer,omitempty"`
	BookID             *int64   `json:"bookID,omitempty"`
	Limit              int      `json:"limit,omitempty" jsonschema:"最终搜索结果的最大数量"`
}

func listAllBooksToolInputSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		Type:                 "object",
		Properties:           map[string]*jsonschema.Schema{},
		AdditionalProperties: &jsonschema.Schema{Not: &jsonschema.Schema{}},
	}
}

type listAllBooksArgs struct{}

type getArgs struct {
	ID string `json:"id" jsonschema:"chunk:<id>、数字切片 id 或 book:<id>"`
}

type statusArgs struct{}

type importArgs struct {
	Path string `json:"path" jsonschema:"Markdown 文件路径"`
}

type queryOutput struct {
	Results []search.Result `json:"results"`
}

type listAllBooksOutput struct {
	Books []bookListItemOutput `json:"books"`
}

type bookListItemOutput struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
}

type getOutput struct {
	ID      string       `json:"id"`
	Content string       `json:"content"`
	Book    *bookOutput  `json:"book,omitempty"`
	Chunk   *chunkOutput `json:"chunk,omitempty"`
}

type bookOutput struct {
	ID         int64  `json:"id"`
	SourcePath string `json:"source_path"`
	Title      string `json:"title"`
	CreatedAt  string `json:"created_at"`
	ModifiedAt string `json:"modified_at"`
	Active     bool   `json:"active"`
}

type chunkOutput struct {
	ID            int64  `json:"id"`
	BookID        int64  `json:"book_id"`
	ParentChunkID int64  `json:"parent_chunk_id"`
	Title         string `json:"title"`
	HeadingPath   string `json:"heading_path"`
	Text          string `json:"text"`
}

type statusOutput struct {
	Books         int `json:"books"`
	Chunks        int `json:"chunks"`
	Embeddings    int `json:"embeddings"`
	ChunksToEmbed int `json:"chunks_to_embed"`
}

type importOutput struct {
	BookID       int64  `json:"book_id"`
	Title        string `json:"title"`
	ParentChunks int    `json:"parent_chunks"`
	Chunks       int    `json:"chunks"`
	Indexed      bool   `json:"indexed"`
}
