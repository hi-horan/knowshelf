# Knowshelf

Knowshelf 是一个 Go 写的个人 Markdown 书籍知识库。它把一本 Markdown 书导入到本地 SQLite，切成 parent/child 两层 chunk，再用 BM25、向量召回、RRF 融合和可选 rerank 做检索，并通过 CLI 和 MCP Streamable HTTP 服务暴露能力。

## Codex 使用示例

![Codex 使用示例](./mcp-use-example.png)

当前核心链路是：

```text
Markdown 导入
  -> parent/child chunk 入库
  -> FTS5/gse + sqlite-vec 召回
  -> RRF 融合
  -> 可选 rerank
  -> CLI/MCP 输出
```

## 项目定位

- 一个 Markdown 文件就是一本书。
- `parent_chunks` 是上下文单元，保存较大的章节或段落文本。
- `chunks` 是检索单元，BM25、embedding、vector search 和 rerank 的召回入口都围绕 child chunk。
- 搜索结果会返回 `chunk_id` 和 `parent_chunk_id`，最终给模型看的正文来自 parent chunk，避免 child 过短导致上下文不足。
- 数据全部落在本地 SQLite，向量索引用 `sqlite-vec` 的 `vec0` virtual table。

## 目录结构

```text
cmd/knowshelf      CLI 入口和应用生命周期
internal/store     SQLite schema、导入、FTS、vector 存储
internal/search    混合检索、RRF、rerank、搜索日志
internal/models    embedding 和 rerank provider
internal/mcp       MCP Streamable HTTP server、工具和中间件
internal/config    YAML 配置结构和 logger
internal/segment   gse 分词与 FTS5 query 构造
```

## 数据模型

主要表：

- `books`：书籍元数据，包含源 Markdown 路径、标题、创建和修改时间。
- `parent_chunks`：上下文 chunk，字段包含 `book_id`、`title`、`heading_path`、`text`。
- `chunks`：child chunk，字段包含 `book_id`、`parent_chunk_id`、`title`、`heading_path`、`text`。
- `chunks_fts`：FTS5 表，用于 BM25 检索。
- `embeddings`：记录哪些 child chunk 已生成 embedding。
- `chunk_vectors`：sqlite-vec 向量表，`rowid = chunks.id`。

## 导入逻辑

CLI import 的流程：

1. 把用户传入的 Markdown 文件复制到 `library.markdown_dir`。
2. 用复制后的 basename 调用 `store.ImportMarkdown`。
3. 标题优先从 frontmatter `title` 读取，其次取第一个 Markdown heading，最后取文件名。
4. 用 Eino Markdown header splitter 按标题结构初分。
5. 用 recursive splitter 生成 parent chunk，默认上限 `2000` runes。
6. 在每个 parent 内继续切 child chunk，默认上限 `1000` runes。
7. 跳过空 chunk 和只有标题的 chunk。
8. 在同一个事务里更新 `books`、`parent_chunks`、`chunks`、`chunks_fts`，并删除旧 embedding/vector 行。

默认 chunk 参数在 `internal/store/chunk.go`：

```text
defaultParentChunkRunes = 2000
defaultChildChunkRunes  = 1000
defaultOverlapRunes     = 0
```

## 检索逻辑

Knowshelf 只有一条混合检索 pipeline：

1. 原始问题走 BM25。
2. 调用方传入的 `rewrittenQueries` 走 BM25。
3. 原始问题、改写问题和 `hypotheticalAnswer` 分别走向量召回。
4. 多路召回结果用 RRF 融合，权重来自 `search.rrf_weights`。
5. 融合时按 parent 聚合，多个 child 命中同一个 parent 会合并贡献。
6. 截断到 `candidate_limit`。
7. 如果启用 rerank，就把 parent 文本发给 rerank provider。
8. 最终分数融合 RRF 位置分和 rerank 分，再按 `min_score` 过滤并按 limit 截断。

## 功能

### CLI

`make` 封装了常用命令：

```bash
make build
make run
make import
make embed
make embed_show
make embed_export
```

### MCP 服务

`run` 命令启动 MCP Streamable HTTP 服务：

```text
MCP endpoint: /mcp
health check: /healthz
```

当前 MCP 工具：

- `query`：按问题检索书籍 chunk，可选 `bookID`、`rewrittenQueries`、`hypotheticalAnswer`、`limit`。
- `listAllBooks`：列出已导入且 active 的书籍 `id` 和 `title`。

客户端接入：

```text
URL: http://127.0.0.1:8765/mcp
Header: Authorization: Bearer <token>
```

先用服务端同一份配置生成只读 MCP token：

```bash
./_output/knowshelf token_gen -c config.yaml --sub codex --scope mcp:read --ttl 24000h
```

如果二进制文件放在其他位置，保持使用同一个 `token_gen` 子命令即可。开发时也可以直接运行 `make token`。

以 Codex MCP client 为例，配置完整的 `/mcp` URL，并把 token 放进 HTTP header：

```toml
[mcp_servers.knowshelf]
url = "http://127.0.0.1:8765/mcp"

[mcp_servers.knowshelf.http_headers]
Authorization = "Bearer <token>"
```

如果从其他机器连接，把 `127.0.0.1` 换成宿主机 IP，并确保端口可以访问。

MCP 支持可选 auth 和 CORS：

- auth 使用 bearer token。
- `token_gen` 可以基于 `mcp.auth.secret` 生成签名 token。
- CORS 配置在 `mcp.cors` 下。

### Embedding 和向量

`embed` 会找出还没有 embedding 的 active child chunks，调用配置的 embedding provider，保存到：

```text
embeddings.chunk_id = chunks.id
chunk_vectors.rowid = chunks.id
```

当前向量维度固定为 `1024`，写入 `chunk_vectors` 前会校验维度。

`embed show` 用于查看向量行和对应 child 文本。`embed export` 会导出 TensorFlow Embedding Projector 可用的：

```text
vectors.tsv
metadata.tsv
```

## 配置

从示例配置开始：

```bash
cp config.example.yaml config.yaml
```

主要配置项：

- `database.path`：SQLite 数据库路径。
- `library.markdown_dir`：导入后 Markdown 文件保存目录。
- `segment.dictionaries`：gse 分词词典。
- `search.default_limit`：默认返回条数。
- `search.candidate_limit`：RRF 后进入 rerank 的候选上限。
- `search.min_score`：最终结果最低分。
- `search.rrf_weights`：各路召回的 RRF 权重。
- `search.enable_vector`：是否启用向量检索和 embedding provider。
- `search.enable_rerank`：是否启用 rerank provider。
- `models.embedding`：embedding provider 配置。
- `models.rerank`：rerank provider 配置。
- `mcp`：HTTP 地址、path、auth、CORS 和 session 配置。
- `observability.trace`：OpenTelemetry trace 配置。

如果只想先跑 BM25，可以把下面两个开关关掉：

```yaml
search:
  enable_vector: false
  enable_rerank: false
```

如果要使用向量检索，需要配置 embedding provider，并保证输出维度是 `1024`。如果要启用 rerank，需要配置 rerank provider 和模型。


## 注意事项

- `chunk_vectors` 是 sqlite-vec 的 `vec0` virtual table，普通 SQLite GUI 如果没有加载 sqlite-vec，可能无法直接打开这张表。
- 重新导入同一本书会替换该书旧的 parent/child chunks、FTS 行和向量行。
