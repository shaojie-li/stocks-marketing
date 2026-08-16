# Stock Market Monitoring

基于 Hyperliquid 公共行情、确定性规则和受约束 AI 解释的 Discord 决策辅助系统。当前不连接钱包、不读取持仓、不自动下单。

## 当前链路

```text
Hyperliquid REST 初始快照 + WebSocket 实时更新
→ Eligibility Gate（完整性、时间戳、双边盘口、OI、断线恢复）
→ 并发安全的内存行情快照

Hyperliquid 同批当前 mark + 24h 起点 candle
→ 8 个同窗口收益 Observation
→ 5 个 Relative Strength + Cross-Market
→ `xyz:SKHY` 已完成 UTC 日线 Price Structure（历史充足时）
→ 按可用权重归一化的 Trend Score

DART SK hynix 公司 RSS 中的衍生品交易损失首次披露
→ 确定性 `BEARISH` Catalyst Event
→ Hyperliquid `xyz:SKHY` / `xyz:SMSN` 同锚点 24h 分钟价格窗口
→ `ACCEPTED` / `NEUTRAL` / `REJECTED` Catalyst Evaluation

固定分析 fixture
→ 同窗口核心指标
→ 符合 global-analysis/1.5.0 的结构化报告
→ PostgreSQL 幂等 Analysis Run
→ River 可靠任务
→ Discord Webhook

通过 Eligibility Gate 的 8 个同窗口 Observation
→ 确定性 Analysis Bundle
→ PostgreSQL 原子保存 Analysis Run、可比 Trend Score 与 Memory session 历史

真实 Analysis Bundle
→ Fundamental / Entry 缺失显式为 `null`
→ `OBSERVE + NO_TRADE + SHADOW_ONLY` 确定性安全门
→ 只读降级全局分析；正式 Discord 投递 fail closed

Hyperliquid `xyz:SKHY` 720 小时 funding/premium + 已完成 UTC 日线
→ 确定性 Crowding 组件、覆盖门与 Evidence
→ 历史 OI 明确 `UNAVAILABLE`；不足 4 项不输出状态
```

缺少核心行情时返回 `SKIPPED_SOURCE_INCOMPLETE`，不会调用 AI 或发送残缺报告。

Analysis Bundle 固定保存稳定分析身份、输入哈希、Observation、六个核心指标、Price Structure、Catalyst Evaluation、Crowding、Trend Score、Memory 结果和数据质量。精确重放返回同一组历史 ID；身份相同但输入不同会显式冲突。Trend 方向只查询规则版本、主资产、phase、window type 和可用组件集合完全相同的前值，Memory 只按 session date 顺序演进。

SKHY Price Structure 只使用 Hyperliquid `xyz:SKHY` 已完成的 UTC `1d` candle。至少 50 根连续已完成日线时计算 EMA20、EMA50、ATR14 和评估日前 20 日支撑低点，并把 Trend 覆盖率从 80% 提升到 90%；历史不足、序列异常或来源失败时保持 `UNAVAILABLE`。2026-08-16 Crowding live check 有 38 根已完成日线，funding、premium、volume 三项可用，但 price extension 与历史 OI 不可用，所以 Crowding 仍为 `UNAVAILABLE`。Foreign Flow 的 KRX 可行性结论为 `NO-GO`：公开 API 不覆盖所需投资者分类，当前项目也没有允许后台派生计算与 Discord 输出的数据合同，因此不接入、不抓取网页私有接口，也不从价格反推。Memory 保持前态、清零 streak，Confidence 上限为 `LOW`。

## 配置边界

所有业务配置都保存在 PostgreSQL 的单表 `app_settings`：

- 普通配置保存为文本；
- OpenAI Key、Discord Webhook 等敏感配置保存为 AES-256-GCM 密文；
- 不使用本地密码 App，不提交 `.env`，不在日志中输出配置值；
- 仅 `DATABASE_URL` 和 `SETTINGS_MASTER_KEY` 作为启动凭据从运行环境注入，因为应用连接和解密数据库前无法从数据库读取它们。

`SETTINGS_MASTER_KEY` 必须是稳定的 32 字节随机值的 Base64 编码。丢失后，数据库内敏感配置无法解密。生产环境应由部署平台注入；不要把它保存回同一数据库或仓库。

## 本地启动

要求：Go 1.26.6、Docker 和 Docker Compose。

```bash
docker compose up -d --wait postgres
export DATABASE_URL='postgres://monitor:monitor@localhost:54329/monitor_test?sslmode=disable'
export SETTINGS_MASTER_KEY="$(openssl rand -base64 32)"
./scripts/migrate.sh
```

开发库使用一次性固定凭据，只监听本机端口。不要把这组凭据用于生产。

配置值统一从标准输入写入，敏感值不会出现在命令参数中：

```bash
go run ./cmd/settings set --secret openai.api_key
go run ./cmd/settings set --secret discord.webhook_url
go run ./cmd/settings set analysis.model
```

每条命令输入值后按 `Ctrl-D`。当前故意不提供“列出全部解密配置”的命令。

启动 worker：

```bash
go run ./cmd/monitor
```

`monitor` 启动时从 `app_settings` 读取 Hyperliquid 端点、关注标的、超时、freshness 和重连边界。断线后会先关闭数据门，完成 REST 重同步和全部订阅确认后才恢复；逐笔行情仅驻留内存。

验证真实公共行情快路径（只读、无需钱包或交易账户）：

```bash
go run ./cmd/market-check
```

命令输出每个标的的 `price/change_pct/timestamp/market_status/source`、必要盘口投影和 Eligibility 结果，不输出配置值或完整订单簿。

验证真实 24 小时合约窗口和六个核心指标（只读）：

```bash
go run ./cmd/indicator-check
```

命令使用同一批当前 mark 和完全相同的分钟起点锚点计算收益，并以相同 `as_of` 检查 `xyz:SKHY` 已完成 UTC 日线。输出 8 个 Observation、派生指标、Price Structure 可用性与 Trend Score，不输出原始 candle 数组。Price Structure 历史达到门槛时 Trend 覆盖率为 90%，否则为 80%；两种情况的 Confidence 上限均为 `MEDIUM`。Foreign Flow 继续不可用，缺失项不补零，也不接第二行情源。

验证当前 DART Catalyst 与 24 小时价格接受（只读）：

```bash
go run ./cmd/catalyst-check
```

当前只识别 SK hynix DART 公司 RSS 中报告名精确为 `파생상품거래손실발생` 的首次披露。DART 没有最近受支持事件时输出 `NO_RECENT_SUPPORTED_EVENT`；更正、补充或撤回输出 `DATA_CONFLICT`。不会回退到 SK hynix Newsroom、搜索、网页识别、媒体或第二价格源，也不会调用 AI、Discord 或交易接口。

验证真实 SKHY Crowding（只读）：

```bash
go run ./cmd/crowding-check
```

命令按官方 500 条上限分页读取 720 小时 funding/premium，并读取完成日线计算价格延伸和 20 日量能。公共 API 没有历史 OI，当前 OI 只保留为来源事实；不足 4 项时输出 `UNAVAILABLE` 和各组件原因。命令不调用 AI、Discord 或交易接口，也不输出完整历史数组。

生成真实降级全局分析（只读 shadow）：

```bash
go run ./cmd/global-analysis-check
```

命令复用真实 Hyperliquid 与 DART 输入，输出符合当前 Schema 的 `OBSERVE / NO_TRADE` 报告。它不调用模型、不写入正式 Analysis Run、不创建 River/Discord delivery；同一降级报告若提交正式投递路径会在任何副作用前被拒绝。

进程收到 `SIGINT` 或 `SIGTERM` 后停止接收任务，并在 15 秒边界内关闭 River 和数据库连接。

## 验证

```bash
go test -count=1 ./...
go vet ./...
go build ./...
TEST_DATABASE_URL="$DATABASE_URL" ./scripts/check-vertical-slice.sh
TEST_DATABASE_URL="$DATABASE_URL" go test -count=1 ./internal/storage/postgres
./scripts/generate.sh
git diff --exit-code
go run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...
docker build -t stock-market-monitoring:test .
```

垂直切片使用真实 PostgreSQL、River 和本地 Discord 假服务，验证并发重复输入只产生一条 Analysis Run、一条业务投递和一次 HTTP 请求，同时检查 Webhook 在数据库中不是明文。

## 关键文档

- [全局分析契约](docs/GLOBAL_ANALYSIS_CONTRACT.md)
- [Hyperliquid 数据源验证](docs/DATA_SOURCE_VALIDATION.md)
- [架构决策](docs/DECISIONS.md)
- [任务索引](docs/TASKS.md)
