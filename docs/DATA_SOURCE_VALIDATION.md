# Hyperliquid 市场数据能力验证

状态：Frozen v1.1
关联任务：[T-002](https://github.com/shaojie-li/stocks-marketing/issues/2)
最近验证：2026-08-15

## 1. 已冻结边界

[D-002](DECISIONS.md#d-002-hyperliquid-是唯一市场数据源缺失时停止处理) 规定 Hyperliquid 是唯一市场数据源。系统不使用 KIS、Massive、BLS、新闻价格或其他供应商补齐缺失行情。

每个阶段先执行 Eligibility Gate：

```text
动态发现全部 perp DEX
→ 精确解析 dex-qualified asset
→ 检查阶段所需 symbol 是否完整
→ 检查 mark/oracle/L2/funding/OI/窗口/freshness
→ 检查流动性门槛
→ 全部通过后才创建 Analysis Run
```

任一步失败：

```text
记录 SKIPPED_SOURCE_INCOMPLETE
→ 不计算指标和 Score
→ 不调用 AI
→ 不发送 Discord 交易报告
```

该 Gate 位于全局分析契约之前，所以不会生成违反 Schema 的残缺报告。缺失不按零分、中性或多空信号处理。

业务名称不能直接与 API symbol 做字符串相等比较。固定映射见 [`asset-map-v1.json`](../testdata/data-sources/hyperliquid/asset-map-v1.json)，每次启动仍需用实时 metadata 验证映射目标存在且可用。

## 2. 验证方法

- 只调用 Hyperliquid 公共只读 Info API 和 WebSocket；
- 设置 15 秒超时，不访问账户、持仓或下单端点；
- 只保存端点、抓取时间、状态、响应 SHA-256 和必要字段投影；
- 不保存 header、token、账户号或用户地址；
- live check 证据：[2026-08-15 公共市场检查](../testdata/data-sources/hyperliquid/2026-08-15-public-market-check.json)。

## 3. 必需标的覆盖

2026-08-15 动态发现默认 DEX 加 9 个 HIP-3 DEX。首次检查错误地只按字面 symbol 精确匹配；复核业务别名后的当前覆盖如下：

| Symbol | 有非零 OI 的候选 | 结论 |
|---|---|---|
| NVDA | `xyz:NVDA` | `AVAILABLE` |
| AMD | `xyz:AMD` | `AVAILABLE` |
| MU | `xyz:MU` | `AVAILABLE` |
| SMH | `xyz:SMH` | `AVAILABLE`，仍需单独检查深度 |
| SKHY | `xyz:SKHY` | `AVAILABLE` |
| SOXX 语义 | `xyz:SMH` | `AVAILABLE_PROXY` |
| QQQ/Nasdaq 语义 | `xyz:XYZ100` | `AVAILABLE_PROXY` |
| SPY/S&P 500 语义 | `xyz:SP500` | `AVAILABLE_PROXY` |
| Samsung | `xyz:SMSN` | `AVAILABLE` |
| KOSPI200 语义 | `xyz:KR200` | `AVAILABLE_PROXY` |
| USDKRW | `xyz:KRW` | `UNAVAILABLE_DELISTED` |
| US2Y / US10Y | 无 | `UNAVAILABLE` |
| Brent | `xyz:BRENTOIL` | `AVAILABLE_PROXY` |
| DXY | `xyz:DXY` | `UNAVAILABLE_DELISTED` |
| WTI | `xyz:CL` | `AVAILABLE_PROXY` |

复核后，六个核心价格指标所需合约均有候选。任一非核心输入缺失时，只跳过依赖它的阶段或模块，不使用其他行情源补齐。

### 3.1 代理语义

- `SMSN` 的 oracle 跟踪 Samsung Electronics 005930.KS 并将 KRW 价格换算为 USD。
- `KR200` 跟踪韩国 200 指数篮子，是 KOSPI200 语义代理，不得标成 KOSPI 综合指数原值。
- `XYZ100` 跟踪 100 家大型非金融美国公司，是 Growth/Nasdaq 语义代理，不得标成 QQQ 原值。
- `SMH` 是非杠杆半导体 ETF，用于 Semiconductor benchmark。
- `SOXL` 虽存在且有 OI，但每日目标为半导体指数的 3 倍且每日重置；不适合作为未经变换的 Relative Strength 基准，因此明确拒绝该替代。
- `SP500`、`BRENTOIL` 和 `CL` 分别承载 S&P 500、Brent 和 WTI 语义；`KRW` 与 `DXY` 虽能从 metadata 发现，但已经 `isDelisted=true` 且 OI 为零，Eligibility Gate 必须拒绝。

## 4. 动态发现与同名合约

`perpDexs` 返回 `xyz`、`flx`、`vntl`、`hyna`、`km`、`abcd`、`cash`、`para`、`mkts`。`xyz` 中重点合约 OI 非零：

| 合约 | mark | oracle | funding | OI |
|---|---:|---:|---:|---:|
| `xyz:NVDA` | 224.5 | 224.52 | 0.00000625 | 738094.088 |
| `xyz:AMD` | 513.85 | 513.95 | 0.00000625 | 22180.554 |
| `xyz:MU` | 973.32 | 972.93 | 0.0000501728 | 142790.9 |
| `xyz:SMH` | 动态值 | 动态值 | 动态值 | 2735.562 |
| `xyz:SKHY` | 166.31 | 166.26 | 0.0000243805 | 1399435.78 |

别名复核时 `xyz:KR200`、`xyz:SOXL`、`xyz:XYZ100` 和 `xyz:SMSN` 均返回非零 OI 与双边盘口。选择 SMH 而非 SOXL 是指标语义决定，不是覆盖缺失。

`flx:NVDA`、`km:NVDA`、`km:MU`、`cash:NVDA`、`mkts:NVDA`、`mkts:MU` 同名但 OI 为零。由此禁止只按 ticker 选择合约，也不能永久硬编码 `xyz`。

## 5. 周末 REST 与 WebSocket

抓取发生在周六。REST `l2Book` 对 `xyz:NVDA`、`xyz:AMD`、`xyz:MU`、`xyz:SKHY` 均返回带服务端毫秒时间戳的双边盘口。

WebSocket 对 `xyz:SKHY` 的 `l2Book` 和 `activeAssetCtx` 订阅成功返回：

- 20 档双边盘口及服务端时间戳；
- funding、OI、日成交量、premium；
- oracle、mark、mid 和 impact prices。

这证明合约场所周末存在连续行情，只能标记 `CONTINUOUS`，不能描述为底层美韩现货市场 OPEN。

## 6. 50 bps 可成交深度

按 Entry Score 的单边最小深度 100,000 USD 门槛：

| 合约 | Bid 深度 | Ask 深度 | 双边最小值 | 是否达标 |
|---|---:|---:|---:|---|
| `xyz:NVDA` | 283,542.61 | 313,733.83 | 283,542.61 | 是 |
| `xyz:AMD` | 65,777.76 | 76,369.01 | 65,777.76 | 否 |
| `xyz:MU` | 116,800.39 | 110,754.96 | 110,754.96 | 是 |
| `xyz:SKHY` | 308,950.83 | 294,394.43 | 294,394.43 | 是 |

深度是时点数据，不能把本次通过永久写入配置。AMD 本次虽有非零 OI 和双边盘口，仍不满足执行资格。

## 7. 已验证失败行为

- 不存在的 `xyz:DOES_NOT_EXIST` 请求返回 HTTP 200 和 JSON `null`；
- 未带 DEX 的 `NVDA` 请求返回 HTTP 200 和 JSON `null`；
- 因此客户端必须同时校验 HTTP 状态、JSON 类型和非空 payload，不能把 200 等同于可用数据。

## 8. Candle 窗口

`candleSnapshot` 的现场结果：

- NVDA、AMD、MU、SKHY 连续 8 天的 1 小时数据各返回 193 根，时间间隔零缺口，其中 56 根开盘时间位于 UTC 周末；
- MU、SKHY 最近 25 小时的 1 分钟数据各返回 1501 根，时间间隔零缺口；
- 1 分钟结果中最后一根仍在形成，只有 `T <= as_of` 的 candle 可以作为已完成窗口终值；
- 不存在的合约返回 HTTP 500 和 `null`，不支持的 `7m` interval 返回 HTTP 422。

结论：Hyperliquid 连续 candle 能提供阶段窗口的原始价格，但开盘、收盘、隔夜和周末是报告调度语义，不是底层现货 session。每个 phase 必须显式保存 `window_start`、`window_end`、`as_of` 和 candle 完成状态。

T-005 在 2026-08-15 的真实复核发现，`xyz:SMH` 最新 1 分钟 candle 曾落后当前 mark 约 100 分钟。因此 candle 尾值不能冒充当前价格，也不能要求 24 小时内每一分钟都连续后再计算收益。冻结实现为：同一次 `metaAndAssetCtxs` 响应中的当前 mark 作为共同终点；理论起点之前最近一根已完成 1 分钟 candle 的 close 作为共同起点，锚点偏差必须小于 60 秒。8 个标的必须具有完全相同的实际起点；任一锚点或当前 mark 缺失即整批返回 `SKIPPED_SOURCE_INCOMPLETE`。

系统同时保存 `theoretical_start`、`window_start`、`window_end`、`baseline_price` 与 `baseline_at`，禁止插值、前向填充、使用 `prevDayPx`、使用过时末根 candle 或补接第二数据源。该窗口描述 Hyperliquid 连续合约，不代表美股或韩股正式现货交易时段。

## 9. 失败与恢复边界

冻结策略见 [`failure-policy-v1.json`](../testdata/data-sources/hyperliquid/failure-policy-v1.json)：

- REST 按端点权重使用每分钟 600 的内部预算，只占官方 1,200 weight/min 上限的一半；不通过主动制造 429 验证公共服务。
- 单次请求 15 秒超时；408、429 和选定 5xx 最多共尝试 3 次，使用 1 秒起步、4 秒封顶的 full-jitter 指数退避。其他 4xx 和载荷校验失败不重试。
- WebSocket 每 30 秒发送 ping，10 秒未收到 pong 即断开；重连退避上限 30 秒。重连后必须取得订阅确认和新快照，恢复前 Gate 拒绝分析。
- `isDelisted=true`、零 OI、mark/oracle 缺失、空或单边盘口、传输超过 5 秒未更新都会产生 `SKIPPED_SOURCE_INCOMPLETE`。
- `perpsAtOpenInterestCap` 中的合约禁止建立新 Entry；`perpDexStatus` 只描述 DEX 状态，不能替代逐资产 eligibility 检查。

`metaAndAssetCtxs` 没有给出外部 oracle 源自身的采样时间。因此系统只能验证盘口/消息的服务端时间和本地接收年龄，不能把“刚收到 oraclePx”描述成“底层 oracle 已确认新鲜”。外部 oracle freshness 为 `UNAVAILABLE` 时，报告置信度不能高于底层证据；出现消息停更或无效字段时直接关闭 Gate。

## 10. 持久化与再分发边界

项目政策冻结为：

- 允许内部拉取公共行情，并保存用于审计的有界聚合、异常、时间戳和响应哈希；不永久保存 tick 流或完整订单簿历史。
- Discord 只发送派生分析和必要的来源/时间戳，不提供原始行情 feed、批量 candle 或订单簿转储。
- trade.xyz Terms 会将 Interface 与第三方服务区分开，但未提供明确的原始市场数据再分发授权。因此 raw redistribution 标记为 `UNCONFIRMED`，在取得书面授权或法律复核前禁止。
- 地域、受限主体及第三方服务条款仍由实际运营者负责核验；本结论是项目风险边界，不是法律意见。

## 11. 可重放验证

公共只读探针只依赖 Python 标准库，不读取账户、地址、token 或 header：

```bash
python3 scripts/probe-hyperliquid.py > /tmp/hyperliquid-probe.json
python3 scripts/validate-analysis-contract.py
```

T-004 增加 Go 运行时验证：REST 的 metadata/context 只按同一响应的数组下标配对并校验等长，数值保留原始十进制字符串；初始快照整批原子写入内存。WebSocket 断线时 Gate 立即关闭，必须重新取得 REST 快照以及每个标的的 `l2Book`、`activeAssetCtx` 订阅确认后才恢复。

快照的 `price` 使用 Hyperliquid `markPx`；`change_pct = (markPx - prevDayPx) / prevDayPx × 100`，以精确十进制计算并保留 8 位小数。`market_status` 固定描述合约场所为 `CONTINUOUS`，不能据此声称美韩底层现货市场处于 OPEN。

行为配置只从 `app_settings` 读取：端点、关注标的、请求超时、5 秒 freshness、重连上下限。可执行以下只读现场检查：

```bash
go run ./cmd/market-check
go run ./cmd/indicator-check
```

探针实时复核版本化映射、退市、OI、mark/oracle、双边盘口和 OI cap。429、超时、断线与 stale 的降级策略由确定性 fixture 校验；不以破坏公共服务或等待真实故障作为验收手段。

## 12. 结论

T-002 的数据源边界可冻结：六个核心指标存在统一 Hyperliquid 映射；非核心 USDKRW、DXY 和美债收益率当前不可用，依赖它们的阶段停止处理而不补源。所有可用性均须在运行时重新发现和过 Gate，本次快照不构成永久保证。

## 13. 官方参考

- [Hyperliquid Info endpoint](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/info-endpoint)
- [Hyperliquid Perpetuals API](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/info-endpoint/perpetuals)
- [Hyperliquid WebSocket subscriptions](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/websocket/subscriptions)
- [Hyperliquid rate limits](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/rate-limits-and-user-limits)
- [Hyperliquid WebSocket](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/websocket)
- [Hyperliquid WebSocket timeouts and heartbeats](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/websocket/timeouts-and-heartbeats)
- [trade.xyz Specification Index](https://docs.trade.xyz/consolidated-resources/specification-index)
- [trade.xyz Korea assets](https://docs.trade.xyz/asset-directory/korea)
- [trade.xyz Equity indices](https://docs.trade.xyz/xyz-perps-specification/equity-indices)
- [trade.xyz Terms of Use](https://trade.xyz/terms)
