# 全局交易分析契约

状态：Frozen v1.0.0
规则版本：`global-analysis/1.0.0`
更新日期：2026-08-15

## 1. 目标与边界

本契约把全局分析中的计算、状态和质量规则从 Prompt 中移到确定性领域逻辑。后续开盘、收盘、隔夜、周末和周一展望报告必须复用这些定义，只允许改变分析窗口和叙事重点。

当前系统提供可追溯的 Discord 决策辅助，不连接钱包、券商私有交易接口或自动下单。模型只解释已经计算的事实、证据和状态，不负责计算权威数值、补齐缺失数据或改变确定性结论。

为消除原始规则中的歧义，v1 做出以下决定：

- 最终报告必须同时输出 Fundamental、Trend、Entry 三个 Score。
- `SKHY Alpha` 拆为 `SKHY Sector Alpha` 和 `SKHY Market Alpha`，不再使用含义不明的合成字段。
- 所有收益率字段使用百分点；例如 `1.25` 表示 `1.25%`，Relative Strength 的单位为 `pp`。
- 所有价格、收益率和资金流计算使用十进制定点数；持久化不得以 `float64` 作为权威值。
- `UNAVAILABLE` 是可用性，不是市场方向；Schema 使用 `availability` 表达，状态值在不可用时为 `null`。

## 2. 时间、市场状态与分析身份

### 2.1 时间要求

- 所有持久化时间戳使用 UTC 和 RFC 3339；报告同时保存分析市场的本地 session date。
- 每次分析固定 `as_of`，任何晚于 `as_of` 的数据不得进入该次结果。
- 同一指标的两个成分必须使用相同 `window_type`、相同基准语义、相同调整口径和兼容的 session date。
- 实时配对行情的时间戳偏差不得超过 120 秒；正式收盘数据必须来自同一个已完成 session。
- Cross-Market 必须分别保存美股和韩股 session date 及 `lag_hours`，不得把非同步市场描述成同一时刻行情。

### 2.2 市场状态

传统市场使用：

- `PREMARKET`
- `OPEN`
- `AFTERHOURS`
- `CLOSED`

连续交易场所使用 `CONTINUOUS`。不能用盘前、盘后或连续合约价格覆盖正式收盘字段。

### 2.3 分析窗口

| 窗口 | 起点 | 终点 | 用途 |
|---|---|---|---|
| `REGULAR_SESSION` | 上一正式交易日收盘 | 当前或最近正式交易时段的 `as_of`/正式收盘 | 日内、收盘和跨市场日度比较 |
| `PREMARKET` | 上一正式交易日收盘 | 当前盘前 `as_of` | 盘前变化；必须同时保留 Previous Close |
| `AFTERHOURS` | 当日正式收盘 | 当前盘后 `as_of` | 盘后变化；不得改写 Regular Close |
| `OVERNIGHT` | 目标市场上一正式收盘 | 下一正式开盘或开盘前最后可验证价格 | 隔夜传导 |
| `CONTRACT_24H` | `as_of - 24h` 的连续场所价格 | 连续场所 `as_of` | Hyperliquid 周末及 24 小时行情 |
| `WEEK_TO_DATE` | 上一交易周最后正式收盘 | 当前 `as_of` 对应的同类价格 | 周内趋势 |

`REGULAR_SESSION` 使用 close-to-close 语义而不是 open-to-close，从而保留跳空对持仓和消息接受度的影响。

### 2.4 稳定分析身份

同一分析的幂等键由以下字段组成：

```text
rule_version + phase + primary_asset + window_type + window_start + window_end + as_of_bucket
```

相同幂等键和相同输入哈希必须返回同一 Analysis Run；相同幂等键但输入哈希不同必须显式报冲突，不得覆盖历史结果。

## 3. 行情 Observation 契约

每条行情至少包含：

```text
symbol
price
change_pct
observed_at
market_status
window_type
session_date
source
source_tier
freshness
adjustment
```

其中：

- `source_tier`：`OFFICIAL`、`EXCHANGE`、`MARKET_API`、`RELIABLE_MEDIA`、`INDUSTRY_DATA`、`SECONDARY`、`DERIVED`。`DERIVED` 只能用于领域引擎产物，并必须保留原 Observation 或 Evidence 引用。
- `freshness`：`REALTIME`、`DELAYED`、`EOD`、`STALE`。
- `adjustment`：`ADJUSTED`、`UNADJUSTED`、`NOT_APPLICABLE`。

实时或准实时来源超过其配置的预期延迟、EOD 来源不是最近一个已完成 session，或时间戳与报告阶段不匹配时标记 `STALE`。数据源不能提供明确 session 时，不得由价格形态猜测市场状态。

### 3.1 事件 Evidence

每条新闻、披露或产业事件必须保存 `published_at`、`event_at`、`source`、`original_source`、`category`、`affected_assets` 和 `importance`。今天发布但描述旧事件的内容按 `event_at` 判断是否为新增 Catalyst，不能按文章发布日期伪装成今日事件。

宏观事件必须额外保存 `actual`、`consensus`、`previous`、`revision` 和 `unit`；缺少任一字段时使用 `null` 并降低完整度，不删除字段。产业事实使用 `CONFIRMED`、`REPORTED`、`ANALYST_ESTIMATE` 或 `RUMOR`，不得把预测、媒体报道或传闻描述成实际成交数据。

Fed 相关 Evidence 还必须使用 `interpretation_type` 区分 `OFFICIAL_POLICY`、`OFFICIAL_DATA`、`MARKET_EXPECTATION` 和 `MEDIA_INTERPRETATION`。Fed Funds Futures 或 CME FedWatch 只能标记为市场预期，不能标记为 Fed 官方决定。

## 4. 六个核心指标

设 `R(symbol, window)` 为同一分析窗口内的收益率，单位为百分比。

| 指标 | 公式 | 必需输入 |
|---|---|---|
| Memory Relative Strength | `R(MU) - R(SOXX)` | MU、SOXX |
| Semiconductor Relative Strength | `R(SOXX) - R(QQQ)` | SOXX、QQQ；QQQ 是 Nasdaq 交易代理，不冒充 Nasdaq 指数值 |
| AI Compute Rotation | `R(AMD) - R(NVDA)` | AMD、NVDA |
| SKHY Sector Alpha | `R(000660.KS) - R(005930.KS)` | SK Hynix、Samsung |
| SKHY Market Alpha | `R(000660.KS) - R(KOSPI)` | SK Hynix、KOSPI |
| Cross-Market Confirmation | 比较 Memory RS 与 SKHY Sector Alpha 的方向和强度 | 两个已计算指标及各自 session date |

禁止从新闻或第三方评论读取已经计算好的 Relative Strength。任一输入不可用、过期、窗口不一致或存在未解决的关键冲突时，指标 `availability` 不是 `AVAILABLE`，`value_pp` 和 `state` 必须为 `null`。

### 4.1 Relative Strength 状态阈值

| 状态 | `value_pp` |
|---|---|
| `STRONG` | `>= 1.00` |
| `POSITIVE` | `>= 0.25` 且 `< 1.00` |
| `NEUTRAL` | `> -0.25` 且 `< 0.25` |
| `WEAK` | `<= -0.25` |

状态只概括方向，原始 `value_pp` 必须始终保留。`WEAK` 不表示所有负值强度相同。

### 4.2 Cross-Market

先把 `STRONG/POSITIVE` 映射为正方向、`WEAK` 映射为负方向、`NEUTRAL` 映射为零方向：

- 两项均非零且同方向：`CONFIRMED`，并输出 `BULLISH` 或 `BEARISH`。
- 两项均非零且方向相反：`DIVERGENCE`。
- 至少一项为 `NEUTRAL`：`NEUTRAL`。
- 任一项不可用：状态为 `null`，`availability` 说明 `UNAVAILABLE`、`STALE` 或 `DATA_CONFLICT`。

## 5. 资金流

资金流质量状态：

- `CONFIRMED`：交易所、监管披露或供应商明确标记的最终数据。
- `PRELIMINARY`：盘中估算或尚未最终结算的数据。
- `UNAVAILABLE`：无法取得、超过时效或口径不明。

方向必须根据实际净买卖计算，不得由股价反推：

```text
flow_ratio_pct = net_buy_value / traded_value * 100
```

- `NET_BUY`：`flow_ratio_pct >= 2.00`
- `NEUTRAL`：`-2.00 < flow_ratio_pct < 2.00`
- `NET_SELL`：`flow_ratio_pct <= -2.00`

个股、KOSPI 市场和 KOSPI 200 Futures 的流量分别保存。期货“当日净买卖”和“未平仓仓位变化”不是同一指标，禁止合并。只有净买卖可用时，仓位必须标记 `UNAVAILABLE`。

## 6. Catalyst 价格接受

每个 Catalyst 必须保存 `expected_direction`、`event_at`、事件前最后可交易参考价、评估窗口和基准资产。先计算：

```text
direction_sign = +1 for BULLISH, -1 for BEARISH
directional_return = direction_sign * target_return_pct
directional_alpha = direction_sign * (target_return_pct - benchmark_return_pct)
```

- `ACCEPTED`：`directional_return >= 0.50` 且 `directional_alpha >= 0.25`。
- `REJECTED`：`directional_return <= -0.50` 且 `directional_alpha <= -0.25`。
- 其他有效结果：`NEUTRAL`。
- 缺少事件方向、事件前价格、目标价格或基准价格：不可用，不猜测。

评估默认截至事件后的第一个完整正式交易时段；该时段未结束时结果标记为 preliminary。若存在看多 Memory Catalyst，同时 Memory RS 为 `STRONG`、两个 SKHY Alpha 均为 `WEAK` 且外资为 `NET_SELL`，必须输出 `Cross-Market = DIVERGENCE` 和 `Catalyst = REJECTED`。

## 7. Crowding

Crowding 使用交易拥挤证据，不使用新闻数量。v1 维护五个输入：

1. 资金费率绝对值在过去 30 天的 percentile；
2. 5 日 OI 增幅在过去 60 个有效观察日的 percentile；
3. 合约相对 oracle/参考现货 premium 绝对值的 30 日 percentile；
4. 价格距 EMA20 的绝对距离除以 ATR14；
5. 成交量在过去 20 个有效交易日的 percentile。

触发条件分别为：percentile `>= 75`、OI 增长为正且 percentile `>= 75`、premium percentile `>= 75`、价格延伸 `>= 1.5 ATR`、成交量 percentile `>= 75`。

| 状态 | 规则 |
|---|---|
| `LOW` | 0 个触发 |
| `NORMAL` | 1–2 个触发 |
| `HIGH` | 3–4 个触发 |
| `EXTREME` | 5 个触发，或 4 个触发且价格延伸 `>= 2.5 ATR` |

至少 4 个输入可用才计算状态，否则 `availability = UNAVAILABLE`。方向由 funding、premium 和价格延伸方向的多数决定为 `LONG`、`SHORT` 或 `MIXED`；状态与方向分开保存。

## 8. 三个 Score

### 8.1 通用规则

- Score 范围为 `0.0–10.0`，保留一位小数。
- 每个 Score 保存组件、权重、组件值、证据引用和覆盖率。
- 覆盖率低于 70% 时 `value = null`，缺失不按零分处理。
- 覆盖率达到 70% 时按可用权重归一化；Confidence 必须相应降低。
- 方向只和“相同 phase、window_type、规则版本及可比组件集合”的上一报告比较：差值 `>= 0.5` 为 `UP`，`<= -0.5` 为 `DOWN`，否则 `FLAT`。
- 仅因数据覆盖集合变化不得产生 `UP/DOWN`。

### 8.2 Fundamental Score

Fundamental 衡量中期经营和产业事实，不使用短线价格。组件权重：

| 组件 | 权重 | 0 分 | 满分 |
|---|---:|---|---|
| AI/HBM/Server 需求 | 2 | 明确收缩 | 多个一手来源确认加速 |
| DRAM/HBM/NAND 价格周期 | 2 | 价格和订单恶化 | 实际价格、订单或公司指引共同改善 |
| 供给与产能纪律 | 2 | 无纪律扩产或库存恶化 | 供给受控且库存改善 |
| 盈利、指引与修正 | 2 | 指引下调/盈利恶化 | 指引上调且盈利修正改善 |
| 资产负债与 CapEx 执行风险 | 1 | 融资或执行风险高 | 资金和执行能力可验证 |
| 监管、客户集中与事件风险 | 1 | 风险已实质发生 | 无重大新增风险或风险缓释 |

每个组件只接受带 `CONFIRMED/REPORTED/ANALYST_ESTIMATE/RUMOR` 证据等级的规范化事实；`RUMOR` 不得单独形成正向分数。模型可以解释组件，不得自行更改组件值。

规范化事实由版本化事件规则映射为 `STRONG_NEGATIVE`、`NEGATIVE`、`NEUTRAL`、`POSITIVE`、`STRONG_POSITIVE`，分别获得该组件权重的 `0%`、`25%`、`50%`、`75%`、`100%`。`STRONG_POSITIVE` 至少需要两个相互独立的一手证据，或一个一手证据加一个已经实现的实际经营结果；只有分析师预测时最高为 `POSITIVE`，只有 Rumor 时组件不可用。没有对应事件映射规则时组件为 `null`，禁止模型临时选择档位。

### 8.3 Trend Score

Trend 只衡量趋势质量，与是否值得追价分开：

| 组件 | 权重 |
|---|---:|
| Memory RS | 2 |
| Semiconductor RS | 1 |
| AI Compute Rotation | 1 |
| SKHY Sector Alpha | 2 |
| SKHY Market Alpha | 1 |
| Cross-Market | 1 |
| SKHY 价格结构 | 1 |
| SKHY Foreign Flow | 1 |

Relative Strength 类组件映射为：`STRONG=100%`、`POSITIVE=75%`、`NEUTRAL=50%`、`WEAK=0%`。Cross-Market 映射为：看多 `CONFIRMED=100%`、`NEUTRAL=50%`、`DIVERGENCE=0%`、看空 `CONFIRMED=0%`。价格结构映射为 `ABOVE_SUPPORT=100%`、`RANGE=50%`、`BROKEN=0%`。Foreign Flow 映射为 `NET_BUY=100%`、`NEUTRAL=50%`、`NET_SELL=0%`。

### 8.4 Entry Score

Entry 衡量当前交易位置，允许出现 `Trend UP + Entry DOWN`：

| 组件 | 权重 | 规则摘要 |
|---|---:|---|
| 延伸/回踩结构 | 3 | 健康回踩并承接 3；普通区间 2；明显延伸 0.5；结构跌破 0 |
| 失效位对应的盈亏比 | 2 | `>=3` 为 2；`>=2` 为 1.5；`>=1.5` 为 1；更低为 0 |
| Catalyst 价格接受 | 2 | `ACCEPTED=2`、`NEUTRAL=1`、`REJECTED=0` |
| 流动性与波动可执行性 | 1 | 正常 1；受限/异常 0 |
| Crowding | 2 | `LOW=2`、`NORMAL=1.5`、`HIGH=0.5`、`EXTREME=0` |

用户持仓方向不是任何 Score 的输入。

价格结构使用正式收盘、EMA20、EMA50、ATR14 和最近一个已确认 20 日 swing low：

- `ABOVE_SUPPORT`：收盘 `>= EMA20` 且 `EMA20 >= EMA50`。
- `BROKEN`：收盘低于 `min(EMA20, swing_low) - 0.25 * ATR14`。
- 其他有效情况为 `RANGE`。
- `HEALTHY_PULLBACK_WITH_BID`：Trend 不为下降，日内最低价进入 `EMA20 ± 0.5 * ATR14`，正式收盘重新站上 EMA20，且收盘位于当日振幅上半区。
- `EXTENDED`：`abs(close - EMA20) / ATR14 >= 2.0`；达到该条件时延伸/回踩组件最高只能得 0.5 分。

流动性与波动可执行性要求参考股票和实际交易合约均未停牌或 stale。KRX 股票还需 20 日成交额中位数 `>= 200 亿 KRW`、有效价差 `<= 0.30%`、`ATR14 / close <= 6%`；Hyperliquid 合约还需 50 bps 双边可成交深度 `>= 100,000 USD` 且有效价差 `<= 0.30%`。任一执行腿不满足即为受限，组件得 0 分。

## 9. Memory 趋势状态机

状态顺序固定为：

```text
WEAK → IMPROVING → CONFIRMED → STRONG → PERSISTENT_STRONG
```

每个已完成正式交易日计算一次 day classification：

- 正向证据：Memory RS、Semiconductor RS、SKHY Sector Alpha、SKHY Market Alpha 为正；Cross-Market 看多确认；Foreign Flow 为 `NET_BUY`。
- 负向证据：上述四项为 `WEAK`；Cross-Market 为背离或看空确认；Foreign Flow 为 `NET_SELL`。
- `SUPPORTIVE`：至少 4 项正向、最多 1 项负向，且 Catalyst 不是 `REJECTED`。
- `ADVERSE`：至少 4 项负向，或出现 Cross-Market 背离且 Foreign Flow 为 `NET_SELL`。
- 其他为 `NEUTRAL_DAY`。

升级所需连续 `SUPPORTIVE` 日数按阶段重新计数：

| 当前状态 | 下一状态 | 连续日数 |
|---|---|---:|
| `WEAK` | `IMPROVING` | 2 |
| `IMPROVING` | `CONFIRMED` | 3 |
| `CONFIRMED` | `STRONG` | 3 |
| `STRONG` | `PERSISTENT_STRONG` | 5 |

任意状态连续 2 个 `ADVERSE` 日下降一级。若同时出现 Cross-Market 背离、Foreign Flow 净卖，并且 Catalyst `REJECTED` 或 SKHY 价格结构 `BROKEN`，构成 `HARD_FAILURE`，当日下降一级。任何单日最多迁移一级。

- `NEUTRAL_DAY`：状态不变，正负 streak 清零。
- 休市：状态和 streak 均不变，不计为交易日。
- 关键数据缺失或冲突：状态不变，streak 清零，Confidence 降低。
- 发生迁移后对应 streak 清零，并保存 from、to、原因、session date 和规则版本。

## 10. 跨市场确认链

固定维护以下六层：

1. MU 跑赢 SOXX；
2. SKHY 跑赢 Samsung；
3. SKHY 跑赢 KOSPI；
4. 外资净买 SKHY；
5. 价格接受当前 Catalyst；
6. Trend Score 相对可比前次报告提高。

每层输出 `PASS`、`FAIL` 或 `UNAVAILABLE` 及证据引用。`first_break` 是首个不是 `PASS` 的层；不能跳过断裂层直接宣称整条链确认。

## 11. 数据冲突、完整度与 Confidence

来源优先级：

```text
官方原始数据
> 交易所/公司披露
> 实时行情 API
> Reuters/Bloomberg 授权数据
> 专业产业数据
> 其他媒体
> 二手快讯
```

高优先级来源可以裁决低优先级差异，但原始值和裁决理由必须保留。同级权威来源无法裁决时标记 `DATA_CONFLICT`，受影响指标不可计算。

Data Completeness 按本次 phase 的必需输入权重计算：

- `HIGH`：`>= 90%`
- `MEDIUM`：`>= 70%` 且 `< 90%`
- `LOW`：`< 70%`

报告 Confidence 不能高于底层数据质量：

- `HIGH`：Completeness 为 HIGH、无关键 stale/conflict、关键资金流为最终数据。
- `MEDIUM`：Completeness 至少 MEDIUM、无未解决关键冲突；存在 preliminary 数据时最高为 MEDIUM。
- `LOW`：Completeness LOW、任一关键输入 stale、存在未解决关键冲突，或 Score 不可计算。

整体 Data Freshness 取关键输入中的最差等级，不用最新的一条数据掩盖旧数据。

## 12. 报告与模型边界

机器输出必须通过 [`global-analysis-report.schema.json`](../schemas/global-analysis-report.schema.json)。固定最终字段包括：

- Fundamental、Trend、Entry 分数及方向；
- 六个核心指标；
- 两个 SKHY Alpha；
- Cross-Market、Catalyst、Foreign Flow、Crowding、Memory Trend；
- 确认链及首个断点；
- 当前策略、最佳交易结构和客观失效条件；
- Data Freshness、Data Completeness、Confidence；
- 输入、规则、Prompt、模型和证据版本。

模型输入是已经冻结的 Evidence Bundle。模型输出只能引用存在的 `evidence_id`；质量门必须拒绝未知枚举、缺少失效条件、引用不存在证据、Confidence 高于数据质量或确定性状态与领域引擎不一致的报告。

## 13. 版本变更

阈值、Score 权重、状态迁移、窗口语义或 Schema 必填字段发生变化时必须提升 `rule_version`，保存旧版本，并使用相同测试向量做差异回放。Prompt 文案变化只提升 `prompt_version`，不得静默改变本契约。

## 14. 验证资产

- 完整报告样例：[`example-report.json`](../testdata/global-analysis/v1/example-report.json)
- 场景向量：[`scenarios.json`](../testdata/global-analysis/v1/scenarios.json)
- Relative Strength 边界：[`relative-strength-boundaries.json`](../testdata/global-analysis/v1/relative-strength-boundaries.json)
- Memory 状态迁移：[`memory-state-transitions.json`](../testdata/global-analysis/v1/memory-state-transitions.json)

Schema 使用 JSON Schema Draft-07。仓库通过以下命令执行确定性语义检查与固定版本的 Schema 校验：

```bash
python3 scripts/validate-analysis-contract.py

npx --yes --package ajv-cli@5.0.0 --package ajv-formats@2.1.1 \
  ajv validate --strict=true --multiple-of-precision=2 -c ajv-formats \
  -s schemas/global-analysis-report.schema.json \
  -d testdata/global-analysis/v1/example-report.json
```

`--multiple-of-precision=2` 只处理 JSON number 的二进制浮点校验误差，不改变一位小数的业务约束。GitHub Actions 对每个 PR 和 `main` 推送执行相同检查；T-003 在建立 Go 工具链后继续复用这些测试向量。
