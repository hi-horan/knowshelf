# AGENTS.md

本文件给后续在 Knowshelf 仓库里工作的 agent 使用。默认先看真实代码，再下结论。

## 项目定位

Knowshelf 是一个 Go 写的个人 Markdown 书籍知识库。主链路是：

`Markdown 导入 -> parent/child chunk 入库 -> FTS5/gse + sqlite-vec 召回 -> RRF 融合 -> 可选 rerank -> CLI/MCP 暴露`

一个 Markdown 文件就是一本书。当前正式模型是：

- `books`：书籍元数据。
- `parent_chunks`：上下文单元，保存较大的章节/段落内容。
- `chunks`：检索单元，FTS、embedding、vector、rerank 都围绕 child chunk 做。
- `embeddings.chunk_id = chunks.id`，`chunk_vectors.rowid = chunks.id`。

## 工作方式

- 用户偏好直接、简洁、尊重当前实现。不要用泛 RAG 理论替代源码事实。
- 改代码前先用 `rg` / `nl -ba` 看入口、调用链、测试和配置。
- 不要覆盖用户已有改动；看到 dirty worktree 先理解，不要 reset 或 checkout。
- 不要做过度防御式编程。入口能保证的，中间业务流保持简单。
- 用户明确说“不考虑兼容/迁移”时，按 direct replacement 做，不主动补历史兼容层。
- 做结构重构时优先同 package 拆文件，保持 CLI/API/schema/行为不变。

## 目录边界

- `cmd/knowshelf`：CLI 和应用生命周期。`main.go` 保持薄入口；`app.go` 放 open/close、tracing、withApp/withStore。
- `internal/store`：SQLite、schema、导入、FTS/vector 存储。DDL 放 `internal/store/sql/*.sql` 并用 Go embed，不要重新塞回 Go 字符串。
- `internal/search`：唯一混合检索流程。`search.go` 放公开编排；`retrieval.go`、`rank.go`、`rerank.go`、`logging.go` 按职责放 helper。
- `internal/mcp`：只做 Knowshelf 的 MCP 工具/中间件绑定；协议和 HTTP transport 用官方 `github.com/modelcontextprotocol/go-sdk/mcp`。
- `internal/models`：embedding/rerank provider。
- `internal/config`：YAML config 结构和 logger。
- `cmd/test/chunk.go`：parent-child chunk 行为 demo，不等同于正式导入 schema。

## 配置规则

- `config.example.yaml` 是配置面参考。删掉不用的配置时，要同步删示例。
- 从配置文件来的配置，尽量在使用它的模块直接读配置结构体，不要层层透传。
- `search.Options` 只放请求参数；`default_limit`、`candidate_limit`、`min_score`、`rrf_weights`、`enable_rerank` 这类搜索配置归 `search.Service` 的配置结构体读取。
- 不要在中间层反复检查配置值合法性；配置加载后的值按可信输入处理，除非用户明确要求校验。

## 导入与 chunking

- CLI import 的用户流程应保持清楚：把源 Markdown 复制到 `cfg.Library.MarkdownDir`，再用复制后的 basename 调 `store.ImportMarkdown`。
- `ImportMarkdown` 的简化契约是只传 `Name` 和 `MarkdownDir`。
- chunk 大小默认在 `internal/store/chunk.go`：`defaultParentChunkRunes`、`defaultChildChunkRunes`、`defaultOverlapRunes`。
- 标题逻辑不要随手改。当前标题来自 frontmatter/heading fallback + `opts.Name`。
- parent 是 context，child 是 retrieval unit；结果里要能追到 `parent_chunk_id`。

## 搜索与 MCP

- 搜索保持单一 hybrid pipeline：BM25、向量召回、RRF、可选 rerank、分数过滤、结果截断。
- Query expansion / rewritten queries 这类增强逻辑应保持内部可控，不随意扩大 CLI/MCP 公共接口。
- MCP HTTP 使用 Streamable HTTP，核心入口是 `/mcp`，健康检查是 `/healthz`。
- MCP 中间件顺序保持 `CORS -> auth -> MCP handler`，避免 OPTIONS preflight 被 auth 拦住。
- MCP 工具保持少而清楚：当前主要是 `query` 和 `listAllBooks`。

## 常用命令

- 格式化：`gofmt -w cmd internal` 或 `make fmt`
- 全量测试：`go test ./...` 或 `make test`
- 常用局部测试：`go test ./internal/store ./internal/search ./internal/mcp ./cmd/knowshelf`
- 构建：`make build`
- 运行 MCP 服务：`make run`
- 导入示例书：`make import`
- 生成 embedding：`make embed`
- 查看向量行：`make embed_show`
- 导出 TensorFlow Projector TSV：`make embed_export`

如果 Go build cache 权限报错，可临时使用 `GOCACHE=/private/tmp/knowshelf-go-build` 再跑对应命令。

## 验证习惯

- 小改动至少跑 touched package tests。
- search/store/schema/MCP 这类跨层改动，优先跑 `go test ./internal/store ./internal/search ./internal/mcp ./cmd/knowshelf`，能跑全量就再跑 `go test ./...`。
- schema 改动要同时检查 DDL、读写路径和测试；不要只改 `schema.sql`。
- 结构重构的顺序：`gofmt` -> `rg` 查旧 symbol/旧路径 -> 局部测试 -> 必要时全量测试。
