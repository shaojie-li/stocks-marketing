# Hyperliquid 市场数据能力验证

状态：In Progress
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
| SPY | 无 | `UNAVAILABLE` |
| Samsung | `xyz:SMSN` | `AVAILABLE` |
| KOSPI200 语义 | `xyz:KR200` | `AVAILABLE_PROXY` |
| USDKRW | 无 | `UNAVAILABLE` |
| US2Y / US10Y | 无 | `UNAVAILABLE` |
| Brent | 无 | `UNAVAILABLE` |
| DXY | 只有零 OI 候选 | `UNAVAILABLE` |
| WTI | 只有零 OI 候选 | `UNAVAILABLE` |

复核后，六个核心价格指标所需合约均有候选。SPY、利率、外汇或能源等非核心输入缺失时，只跳过依赖它们的阶段或模块，不使用其他行情源补齐。

### 3.1 代理语义

- `SMSN` 的 oracle 跟踪 Samsung Electronics 005930.KS 并将 KRW 价格换算为 USD。
- `KR200` 跟踪韩国 200 指数篮子，是 KOSPI200 语义代理，不得标成 KOSPI 综合指数原值。
- `XYZ100` 跟踪 100 家大型非金融美国公司，是 Growth/Nasdaq 语义代理，不得标成 QQQ 原值。
- `SMH` 是非杠杆半导体 ETF，用于 Semiconductor benchmark。
- `SOXL` 虽存在且有 OI，但每日目标为半导体指数的 3 倍且每日重置；不适合作为未经变换的 Relative Strength 基准，因此明确拒绝该替代。

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

## 9. 尚未完成

- 429、超时和服务端错误的有限重试边界；
- WebSocket 断线重连、订阅上限和异常关闭；
- 空盘口、合约暂停、oracle stale 和 mark/oracle 偏离规则；
- 公开数据的持久化、展示与二次分发许可。

这些项目完成并形成可重放探针后，T-002 才能关闭。

## 10. 官方参考

- [Hyperliquid Info endpoint](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/info-endpoint)
- [Hyperliquid Perpetuals API](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/info-endpoint/perpetuals)
- [Hyperliquid WebSocket subscriptions](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/websocket/subscriptions)
- [trade.xyz Specification Index](https://docs.trade.xyz/consolidated-resources/specification-index)
- [trade.xyz Korea assets](https://docs.trade.xyz/asset-directory/korea)
- [trade.xyz Equity indices](https://docs.trade.xyz/xyz-perps-specification/equity-indices)
