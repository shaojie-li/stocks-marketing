# 核心数据源能力验证

状态：In Progress
关联任务：[T-002](https://github.com/shaojie-li/stocks-marketing/issues/2)
最近验证：2026-08-15

## 1. 结论先行

当前只能确认无需账户凭据的公共链路，不能据此关闭 T-002：

- Hyperliquid 必须按 `dex + asset` 动态发现合约，并在选用前检查 OI、盘口和 oracle。仅按 `NVDA`、`MU` 或 `SKHY` 名称匹配会选到零 OI 的同名合约。
- `xyz` DEX 在周六返回 NVDA、AMD、MU、TSM 和 SKHY 的 mark、oracle、funding、OI 与双边盘口，可作为币股合约候选源；这只表示连续交易场所有报价，不表示底层美韩现货处于 OPEN。
- 已接受的架构边界见 [D-002](DECISIONS.md#d-002-hyperliquid-统一合约行情外部来源只补分析事实)：Hyperliquid 统一合约行情，外部来源只补分析基准和事实，不进入交易价格路径。
- BLS 公共 API 可以返回官方时间序列值、前一期值和 preliminary footnote，但单次响应不提供市场 Consensus，也没有独立 Revision 字段，不能单独满足宏观事件契约。
- KIS、Massive、Trading Economics、BEA、OpenDART 等需要真实账户或 API Key 的链路尚未完成 entitlement live check。文档存在端点不等于当前账户有权访问。

## 2. 验证方法

所有 live check：

- 设置 15 秒超时，不重试，不访问账户、持仓或下单端点；
- 只保存端点、抓取时间、HTTP/API 状态、响应 SHA-256 和必要字段投影；
- 不保存 header、API Key、token、账户号或用户地址；
- 将无法以真实账户验证的能力标记为 `UNAVAILABLE`，不根据文档推断 entitlement。

证据 fixture：

- [Hyperliquid 公共行情](../testdata/data-sources/hyperliquid/2026-08-15-public-market-check.json)
- [BLS 公共宏观数据](../testdata/data-sources/bls/2026-08-15-public-series-check.json)

## 3. 当前覆盖矩阵

| 来源 | 目标能力 | 文档能力 | Live check | 当前结论 |
|---|---|---|---|---|
| Hyperliquid | 合约发现、mark、oracle、funding、OI、L2、周末行情 | 官方 Info API 提供 `perpDexs`、`metaAndAssetCtxs`、`l2Book` 和 WebSocket | 公共 REST、WebSocket 和 50 bps 深度已成功；429 和历史窗口待测 | `PARTIAL_CONFIRMED` |
| KIS | 韩股/指数/期货/夜盘/投资者净买卖 | 官方目录列出国内股票投资者趋势、国内期货行情和 KRX 夜盘实时行情 | 凭据不存在，未验证账户权限与响应字段 | `UNAVAILABLE` |
| Massive | 美股 ETF、指数、外汇、期货/商品、session | 官方文档分别提供 Stocks、Indices、Forex、Futures 和 market status | API Key 不存在，未验证实际套餐 entitlement | `UNAVAILABLE` |
| BLS | CPI、PPI、NFP 等官方 Actual/Previous | 公共 Data API v2 | 无 Key 请求成功 | `PARTIAL_CONFIRMED` |
| Trading Economics | 宏观 Consensus 与日历 | 候选补充源 | API Key 不存在 | `UNAVAILABLE` |
| BEA | GDP、消费和收入等官方数据 | 官方 API 提供数据及 metadata | UserID 不存在 | `UNAVAILABLE` |
| Fed / Treasury / EIA | 利率、收益率和能源 | 待逐项验证官方机器接口与发布时间 | 尚未执行 | `UNAVAILABLE` |
| SEC EDGAR | 美国公司披露与原文 | 官方 `data.sec.gov` | 尚未配置合规联系信息 User-Agent | `UNAVAILABLE` |
| OpenDART | 韩国公司披露与原文 | 候选官方披露 API | API Key 不存在 | `UNAVAILABLE` |

## 4. Hyperliquid 现场结果

抓取时间为 2026-08-15 07:36 UTC（周六）。`perpDexs` 返回默认 DEX 加 9 个 HIP-3 DEX：`xyz`、`flx`、`vntl`、`hyna`、`km`、`abcd`、`cash`、`para`、`mkts`。

### 4.1 合约发现

`xyz` 返回下列重点合约且 OI 非零：

| 合约 | mark | oracle | funding | OI |
|---|---:|---:|---:|---:|
| `xyz:NVDA` | 224.5 | 224.52 | 0.00000625 | 738094.088 |
| `xyz:AMD` | 513.85 | 513.95 | 0.00000625 | 22180.554 |
| `xyz:MU` | 973.32 | 972.93 | 0.0000501728 | 142790.9 |
| `xyz:TSM` | 424.25 | 424.29 | 0.00000625 | 23792.408 |
| `xyz:SKHY` | 166.31 | 166.26 | 0.0000243805 | 1399435.78 |

同一时刻，`flx:NVDA`、`km:NVDA`、`km:MU`、`cash:NVDA`、`mkts:NVDA` 和 `mkts:MU` 虽然名称存在，但 OI 均为零。由此冻结候选选择规则：

```text
discover perpDexs
→ fetch metaAndAssetCtxs for every dex
→ exact match dex-qualified asset
→ require non-zero OI and usable two-sided book
→ retain oracle/mark divergence and source timestamp
```

不能把 `xyz` 永久硬编码为唯一来源；每次启动和定期刷新都要重新发现。

### 4.2 周末盘口

周六 REST `l2Book` 对 `xyz:NVDA`、`xyz:AMD`、`xyz:MU`、`xyz:SKHY` 均返回带服务端毫秒时间戳的双边盘口。WebSocket 对 `xyz:SKHY` 的 `l2Book` 和 `activeAssetCtx` 订阅也成功返回 20 档双边盘口、funding、OI、日成交量、oracle、mark 和 mid。

因此币股合约自身可标记为 `CONTINUOUS` 候选；报告仍须把底层现货 session 单独标记为 `CLOSED`，并保留 oracle 的更新时间和 stale 判断。

### 4.3 50 bps 可成交深度

按本契约的单边最小深度 100,000 USD 门槛计算，现场结果为：

| 合约 | Bid 深度 | Ask 深度 | 双边最小值 | 是否达标 |
|---|---:|---:|---:|---|
| `xyz:NVDA` | 283,542.61 | 313,733.83 | 283,542.61 | 是 |
| `xyz:AMD` | 65,777.76 | 76,369.01 | 65,777.76 | 否 |
| `xyz:MU` | 116,800.39 | 110,754.96 | 110,754.96 | 是 |
| `xyz:SKHY` | 308,950.83 | 294,394.43 | 294,394.43 | 是 |

深度是时点数据，不能把本次通过永久写入配置。每次生成 Entry Score 和未来执行前都要重算；AMD 本次有非零 OI 和双边盘口，但仍应判定流动性受限。

尚未确认：

- WebSocket 断线重连、订阅上限和异常关闭行为；
- 429、超时、空盘口和合约暂停时的响应；
- 周末 oracle 更新机制、参考市场和 stale 阈值；
- 50 bps 可成交深度是否达到 Entry Score 的 100,000 USD 门槛；
- 数据持久化、展示和二次分发许可。

## 5. BLS 现场结果

无注册 Key 调用 BLS Public Data API v2，`CUUR0000SA0` 与 `CES0000000001` 返回 `REQUEST_SUCCEEDED`。样例包含 2026 年 7 月和 6 月值；就业序列 footnote 明确标记 `preliminary`。

响应能直接提供：

- 时间序列 ID；
- 年、期、数值；
- latest 标记；
- preliminary 等 footnote。

响应不能直接提供：

- 市场 Consensus；
- 独立 Revision 数值；
- 公布前后的市场价格反应。

因此宏观事件的最小组合应是“官方 Actual/Previous + 可验证 Consensus 提供商 + 行情 API 的价格反应”。Revision 需要保存前后两次官方发布快照并确定性比较，不能让模型从新闻措辞猜测。

## 6. 账户凭据状态

本次环境只检查环境变量是否存在，不读取或输出变量值。结果：

```text
KIS: UNAVAILABLE
Massive: UNAVAILABLE
Trading Economics: UNAVAILABLE
BLS registered key: UNAVAILABLE（公共请求不需要）
FRED: UNAVAILABLE
```

这些缺失只降低验证完整度，不形成数据源负面结论。T-002 在 KIS、Massive 和宏观 Consensus 的真实 entitlement 完成前保持 Open。

## 7. 官方参考

- [Hyperliquid Info endpoint](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/info-endpoint)
- [Hyperliquid Perpetuals API](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/info-endpoint/perpetuals)
- [KIS Open API 目录](https://apiportal.koreainvestment.com/apiservice-summary)
- [Massive API 文档](https://massive.com/docs)
- [BLS Public Data API v2](https://www.bls.gov/developers/api_signature_v2.htm)
- [BEA Data API](https://apps.bea.gov/api/signup/)

## 8. 下一检查点

1. 为 Hyperliquid 增加 WebSocket、深度和失败响应探针，明确周末 oracle stale 规则。
2. 在安全环境变量具备后，对 KIS 与 Massive 执行只读 live check，并保存脱敏 fixture。
3. 验证 Trading Economics 或其他授权源能否补齐 Consensus，形成宏观组合源决策。

完成条件仍以 Issue #2 为准；本文的 `PARTIAL_CONFIRMED` 不代表数据源已被最终采用。
