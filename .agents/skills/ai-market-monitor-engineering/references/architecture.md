# 架构规范

## 总体架构

使用 Go 模块化单体。第一阶段运行一个可部署进程，同时保留模块边界，使同一仓库未来可以按 `all`、`market`、`worker` 或 `api` 角色运行，而不需要重写领域逻辑。

使用两条数据链路：

- 可靠链路：处理官方来源发现、原文快照、标准化、事件抽取、实体和资产映射、AI 分析、质量控制及 Discord 投递。
- 内存快路径：处理 Hyperliquid WebSocket、stale 检测、滚动行情状态、异常检测和有界聚合。

仅在为事件关联行情上下文或生成行情异常告警时合并两条链路。

## 已批准技术栈

| 领域 | 标准 |
|---|---|
| 运行时 | 仓库固定当前受支持的 Go 1.26.x 补丁版本 |
| HTTP | 标准库 `net/http` |
| WebSocket | `github.com/coder/websocket` |
| 数据库 | PostgreSQL |
| 驱动和连接池 | `github.com/jackc/pgx/v5` 和 `pgxpool` |
| SQL 代码生成 | 使用显式 SQL 的 `sqlc` |
| 数据库迁移 | `tern` |
| 可靠任务 | 与业务共用 PostgreSQL 的 River |
| 日志 | 标准库 `log/slog`，输出结构化 JSON |
| 指标 | Prometheus Go client |
| 链路追踪 | 渐进接入 OpenTelemetry |
| Discord | 直接调用 Execute Webhook，使用 rich Embed 和固定消息类型路由 |
| 原文快照 | S3 兼容对象存储，PostgreSQL 保存元数据 |
| 本地运行 | Docker Compose |
| 生产部署 | 单镜像、托管 PostgreSQL 和对象存储 |

没有经过实际需求验证并形成已接受决策前，不得引入 Gin、Fiber、Echo、GORM、Redis、Kafka、RabbitMQ、Neo4j、Kubernetes、LangChain 或独立向量数据库。

## 包结构

```text
cmd/monitor/
internal/
  app/
  config/
  domain/
  source/
    policy/
    disclosure/
    company/
  market/hyperliquid/
  ingest/
  event/
  entity/
  relation/
  analysis/
  alert/discord/
  jobs/
  storage/postgres/
  storage/object/
  observability/
queries/
migrations/
testdata/
docs/
```

按业务能力组织包，不使用通用的 controller/service/repository 机械分层。只有其他仓库确实需要公共包时才创建 `pkg/`。不得创建包罗万象的 `common`、`base` 或 `utils` 包。

## 可靠性规则

- 让所有阻塞边界接收并传递 `context.Context`。
- 为长期运行组件建立显式生命周期和优雅关闭。
- 限制 channel、goroutine、worker pool、请求体和重试规模。
- 对每个来源独立限流，并使用带 jitter 的指数退避。
- 使用稳定幂等键和数据库唯一约束。
- 保存来源发布时间、首次发现、抓取、分析和投递时间。
- 记录来源 URL、快照哈希、解析器版本、关系图版本、模型、Prompt 版本和行情上下文时间。
- 明确标记 stale 或不可用行情，绝不将其静默当作当前数据。
- 对 P0 使用两阶段告警：先发送已验证事实，再发送分析更新。
- 不让 LLM 计算权威行情数值，也不允许其将无证据判断转换成事实。

## 数据规则

将核心查询字段规范化为关系型列。只用 JSONB 保存来源特有原始载荷和模型完整输出。

使用实体、别名、金融工具和关系表表达产业图谱。只有具体检索基准证明有价值后才加入 `pgvector`。

摄取时保留外部金融数字的原始字符串，权威值存为 PostgreSQL `NUMERIC`；不得将 `float64` 作为价格、资金费率或持仓量业务判断的持久化依据。

不得永久保存每个订单簿或成交更新。在内存维护最新状态，只持久化有界聚合、异常和告警前后回放窗口。

## 外部系统边界

- 强信号只使用政策机构、监管机构、公司、交易所、项目方或链上的官方一手来源。
- MVP 行情只使用 Hyperliquid 公共 HTTP 和 WebSocket API。
- 通过实时 `meta` 或 `metaAndAssetCtxs` 发现 `xyz` 合约。
- 不抓取 `app.trade.xyz`。
- 决策辅助阶段不连接钱包或私有交易接口。
- 通过小型、供应商无关接口访问模型，并校验结构化输出 Schema。
- 保证 Discord 投递可重试，保存 provider message ID 和每次投递尝试；Webhook token 不进入数据库或日志。
