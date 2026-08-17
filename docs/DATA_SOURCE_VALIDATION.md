# 市场、Foreign Flow 与 Catalyst 数据能力验证

状态：Frozen v1.3
关联任务：[T-002](https://github.com/shaojie-li/stocks-marketing/issues/2)、[T-009](https://github.com/shaojie-li/stocks-marketing/issues/18)、[T-010](https://github.com/shaojie-li/stocks-marketing/issues/20)
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

T-008 使用相同 `candleSnapshot` 读取 `xyz:SKHY` 的 `1d` 连续合约日线。2026-08-15 固定 `as_of = 2026-08-15T06:00:00Z` 的 live check 返回 38 根，其中只有 37 根 close time 严格早于 `as_of`；最后一根仍在形成，必须排除。该证据不足 EMA50 的 50 根门槛，所以真实 Price Structure 为 `UNAVAILABLE / INSUFFICIENT_HISTORY`，不是计算失败或中性信号。脱敏投影与响应哈希见 [`t008-skhy-daily-live-check.json`](../testdata/data-sources/hyperliquid/t008-skhy-daily-live-check.json)。

Hyperliquid 官方当前支持 `1d` interval 且最多返回最近 5000 根 candle。实现从 epoch 起请求可用历史，只保存 Bundle 中的派生结构、必要窗口和响应哈希，不永久保存或再分发完整 candle 数组。日线自然累计到至少 50 根连续已完成记录后自动开始计算，无需补历史、切换来源或修改配置。

## 9. 失败与恢复边界

冻结策略见 [`failure-policy-v1.json`](../testdata/data-sources/hyperliquid/failure-policy-v1.json)：

- REST 按端点权重使用每分钟 600 的内部预算，只占官方 1,200 weight/min 上限的一半；不通过主动制造 429 验证公共服务。
- 单次请求 15 秒超时；408、429 和选定 5xx 最多共尝试 3 次，使用 1 秒起步、4 秒封顶的 full-jitter 指数退避。其他 4xx 和载荷校验失败不重试。
- WebSocket 每 30 秒发送 ping，10 秒未收到 pong 即断开；重连退避上限 30 秒。重连后必须取得订阅确认和新快照，恢复前 Gate 拒绝分析。
- `isDelisted=true`、零 OI、mark/oracle 缺失、空或单边盘口、传输超过 5 秒未更新都会产生 `SKIPPED_SOURCE_INCOMPLETE`。
- `perpsAtOpenInterestCap` 中的合约禁止建立新 Entry；`perpDexStatus` 只描述 DEX 状态，不能替代逐资产 eligibility 检查。

`metaAndAssetCtxs` 没有给出外部 oracle 源自身的采样时间。因此系统只能验证盘口/消息的服务端时间和本地接收年龄，不能把“刚收到 oraclePx”描述成“底层 oracle 已确认新鲜”。外部 oracle freshness 为 `UNAVAILABLE` 时，报告置信度不能高于底层证据；出现消息停更或无效字段时直接关闭 Gate。

## 10. OpenDART Fundamental

2026-08-16 使用加密认证键完成 SK hynix `00164779` 真实只读验证。最新最终定期报告为接收号 `20260814003509`、`반기보고서 (2026.06)`、CFS、报告期末 `2026-06-30`；列表、当期 247 个财务科目、上年同期 238 个科目及同接收号单文件报告 ZIP 均取得独立 SHA-256。脱敏证据见 [`t013-skhy-fundamental-live-check.json`](../testdata/data-sources/opendart/t013-skhy-fundamental-live-check.json)。

结构化科目覆盖 revenue、operating profit、inventory、cash、短长借款、operating cash flow 和 PPE purchases；原始正式报告的窄章节覆盖 HBM/AI server DRAM 实际销售与出货以及 DRAM/NAND ASP 方向。因此五个 Fundamental 组件可确定性计算，覆盖 90%；监管、客户集中与事件风险没有可证明的“无风险”输入，继续不可用。

OpenDART 当前 TLS 端点协商 TLS 1.2 `TLS_RSA_WITH_AES_128_GCM_SHA256`。Go 客户端显式启用该旧套件并限制为官方 host；不调用 curl 子进程、不引入第二语言。无效键、官方状态非正常、HTTP/载荷错误、响应超限、账户重复/缺失、接收号冲突、ZIP 成员冲突或窄章节缺失均显式失败。SK hynix Newsroom 现行条款禁止机器人自动化访问，不作为回退。

## 11. 持久化与再分发边界

项目政策冻结为：

- 允许内部拉取公共行情，并保存用于审计的有界聚合、异常、时间戳和响应哈希；不永久保存 tick 流或完整订单簿历史。
- Discord 只发送派生分析和必要的来源/时间戳，不提供原始行情 feed、批量 candle 或订单簿转储。
- trade.xyz Terms 会将 Interface 与第三方服务区分开，但未提供明确的原始市场数据再分发授权。因此 raw redistribution 标记为 `UNCONFIRMED`，在取得书面授权或法律复核前禁止。
- 地域、受限主体及第三方服务条款仍由实际运营者负责核验；本结论是项目风险边界，不是法律意见。

### 11.1 KRX Foreign Flow 可行性结论

2026-08-15 按 T-009 的 `GO` / `NO-GO` 门复核 KRX 官方资料。结论为 `NO-GO`：KRX 网页和付费数据商品证明投资者分类数据客观存在，但当前公开 OPEN API 不覆盖所需投资者净买卖字段；免费 API 条款也不允许本项目默认进行 Discord 第三方输出。项目没有覆盖该用途的 KRX/Koscom 数据合同，因此不申请认证密钥、不调用未文档化网页接口，也不实施 Foreign Flow。

| 范围 | 官方可见能力 | `flow_ratio_pct` 所需字段 | 自动化与最终性 | 当前结论 |
|---|---|---|---|---|
| SK Hynix 普通股 `000660` | Data Marketplace 提供股票投资者别交易实绩和个股视图，可见卖出、买入与净买入成交金额 | 网页能力表明外资净买额存在；公开 OPEN API 只列出有价证券日别交易信息，未列投资者类别，无法从一个获准接口证明同日同口径的分子与分母 | 股票统计页说明正则市场数据预计 15:45 反映、含盘后交易的最终数据预计 18:00 提供，但没有获准 API 字段区分 `PRELIMINARY` / `CONFIRMED` | `UNAVAILABLE` |
| KOSPI 市场 | Data Marketplace 提供股票市场投资者别交易实绩 | 网页能力存在，但公开 OPEN API 没有市场投资者类别净买卖服务，无法证明可自动取得同口径外资净买额和市场成交额 | 未取得文档化 endpoint、字段规范及产品用途授权 | `UNAVAILABLE` |
| KOSPI 200 Futures | Data Marketplace 提供衍生品投资者别交易实绩；数据商品页明确出售“期货投资者类型别日度交易实绩” | 付费商品可能覆盖外资当日净买卖，但公开 OPEN API 只列出期货日别交易信息，未列投资者类别；未取得商品字段规范与合同 | 未取得文档化、获准的日度接口；最终发布时间、修订规则和 Discord 派生输出授权均未确认 | `UNAVAILABLE` |
| KOSPI 200 Futures OI 变化 | KRX 市场数据商品包含未平仓数量等参考信息 | 未证明可按外资类别取得 OI 变化；全市场 OI 不能替代外资仓位变化 | 净买卖与 OI 是不同指标，禁止合并或推断 | `UNAVAILABLE` |

`000660` 是韩国现货 SK Hynix 普通股，`xyz:SKHY` 是 Hyperliquid 连续合约。即使未来取得 KRX 授权，前者也只能作为 Foreign Flow 非价格事实来源；不得提供、补齐或覆盖后者的任何价格、成交量、OI 或行情状态。

合规阻断来自官方现行规则：

- KRX OPEN API 需要注册、认证密钥、具体服务申请和管理员审批，密钥使用期为一年，每个密钥每日最多 10,000 次请求；公开服务清单没有投资者类别净买卖服务。
- KRX OPEN API 只允许非商业用途，禁止向第三方提供所获信息，并要求使用结果的画面标明“韩国交易所统计信息”。定期 Discord 报告属于第三方系统化输出，不能在没有书面许可时默认视为允许。
- KRX/Koscom 市场数据可以通过合同许可，但非查询型量化分析、加工指标和外部提供有独立申报、计费与审批要求。现有仓库没有该合同，数据商品“可以买”不等于当前项目“已经获准使用”。

因此不执行 live check：没有文档化且获准的目标 endpoint 时，真实请求只能依赖无关 API 或未公开网页接口，既不能证明三类能力，也违反本任务的授权边界。未来只有在 KRX/Koscom 书面合同同时明确覆盖三个范围、后台自动获取、派生计算、必要存储和 Discord 输出后，才重新开启独立可行性评审；缺少任一项都保持 `UNAVAILABLE`。

AI 浏览器或模型视觉识别不构成例外：定时让 AI 打开统计网页仍是自动化获取，不能替代数据许可；动态表格识别也不能提供稳定 Schema、最终状态和可重放计算。用户手动提供的单次截图或导出文件可以用于非权威解释，但必须标明人工来源，不得进入 Analysis Bundle、Trend Score、Memory 或生产历史。

### 10.2 DART Catalyst 来源与授权边界

2026-08-15 按 T-010 复核 DART 官方 RSS 服务。官方说明把 RSS 定义为自动、便捷提供更新的服务，明确提供公司级 feed，并限定为上市公司最近 5 个营业日披露。SK hynix corp code `00164779` 的只读 feed 无需账户或密钥；脱敏一手 fixture 见 [`t010-skhy-company-rss.xml`](../testdata/data-sources/dart/t010-skhy-company-rss.xml)。

本项目只保存必要披露元数据、响应 SHA-256 与派生 Catalyst，不保存正文或对外提供原始 RSS。该边界支持当前只读自动获取与内部派生分析，不代表取得原始披露批量再分发、正文复制或所有商业用途的授权；产品用途扩大时仍需重新审查条款。

现场 feed 提供 `title`、`link/guid`、`pubDate`、`dc:date` 和 `dc:creator`。`rcpNo=20260814802986` 的 `파생상품거래손실발생` 由 `SK하이닉스` 提交，两个时间字段都映射到 `2026-08-14T07:44:00Z`。公司 feed 也可能包含由 `유가증권시장본부` 发起的查询披露；这类 item 保留提交人事实但不参与 SK hynix Catalyst 候选，不能因其 creator 不同而令整个 feed 失败。首个切片据此冻结：

- 唯一事实源为 `https://dart.fss.or.kr/api/companyRSS.xml?crpCd=00164779`；host、路径、corp code、公司、时间和 `rcpNo` 均需校验；
- 只有首次披露 `파생상품거래손실발생` 映射为 `DERIVATIVE_TRADING_LOSS_OCCURRED / CONFIRMED / BEARISH`；其他报告不产生方向；
- 更正、补充或撤回为 `DATA_CONFLICT / REVISION_UNSUPPORTED`；没有最近受支持事件为 `UNAVAILABLE / NO_RECENT_SUPPORTED_EVENT`；
- DART 不可用时不回退到 Newsroom、搜索、网页识别或媒体；DART 是非价格事实源，不改变 Hyperliquid 作为唯一价格行情源的边界；
- 目标 `xyz:SKHY` 与基准 `xyz:SMSN` 只使用 Hyperliquid 完整 `1m` candle 构造 `CATALYST_24H`，任一侧缺失即整项不可用。

SK hynix Newsroom 的现行 Terms 只允许非商业使用，并明确禁止 robot、spider 或其他自动设备访问站点以监控或复制材料。即使 Press RSS 技术上可读，也不能作为本项目的后台生产来源；AI 自动打开网页同样属于自动化获取，不能绕过该限制。

### 10.3 Hyperliquid Crowding 覆盖

2026-08-16 按 T-012 复核官方公共 Info API 与 `xyz:SKHY` 真实响应：

- `fundingHistory` 返回 `coin`、`fundingRate`、`premium` 和毫秒 `time`；资金费用每小时结算。时间范围响应最多 500 条，30 日窗口必须分页。本次从 2026-07-16 起读取两页，分别为 500 与 256 条，所有记录 symbol 一致且 premium 非空。
- `metaAndAssetCtxs` 当前响应同时提供 `funding`、`premium`、`openInterest`、`dayNtlVlm`、`dayBaseVlm`、oracle 和 mark。它没有历史 OI、来源 oracle 自身采样时间或 5 日增长字段，因此只能保存当前 OI 事实，不能计算 OI Crowding。
- `candleSnapshot` 的 `1d` 返回 OHLC、成交量、成交笔数和开闭时间。本次共 39 根，其中最后一根仍形成，只有 38 根完成；20 日 volume 可计算，但 50 根 price extension 历史仍不足。

因此当前真实覆盖是 funding、premium、volume 三项可用，price extension 与历史 OI 两项不可用；Crowding 必须返回 `UNAVAILABLE / INSUFFICIENT_INPUTS`，不能输出 `LOW`。日线累计到 50 根后可以由四项真实输入自动启用。官方历史 S3 约月度上传、不保证及时或完整且由请求方承担传输费用，不作为本阶段生产历史 OI 来源。

现场检查只保存必要字段投影、计数、时间边界和响应 SHA-256，不保存或再分发完整 funding/candle 数组。raw redistribution 继续标记 `UNCONFIRMED`。Foreign Flow 仍是独立的 `NO-GO` 信号，禁止由 funding、premium、OI、成交量或价格延伸反推。

### 10.4 Entry readiness 与确认链边界

2026-08-17 对 Hyperliquid 官方公共 API 做只读复核：`xyz:SKHY` 日线返回 40 根，其中 39 根 close time 严格早于检查时点，最后一根仍在形成；因此 Price Structure 和 Crowding price extension 继续 `UNAVAILABLE / INSUFFICIENT_HISTORY`。同一时点 `l2Book` 返回双边各 20 档，有效价差约 `0.00579828%`，50 bps 内 bid/ask 名义深度分别约 `834,959.58 / 824,278.83 USD`。这些结果只证明当时的 Hyperliquid 合约腿可执行性，不能补齐韩国现货参考腿或永久保证未来流动性。脱敏投影与响应哈希见 [`t014-entry-readiness-live-check.json`](../testdata/data-sources/hyperliquid/t014-entry-readiness-live-check.json)。

当前完整 Entry 结论为 `NO-GO`：日线不足 50 根，正式交易时段健康回踩规则没有唯一价格源下的获准输入，韩国现货参考腿也不可用。不得降低门槛、把连续合约 UTC 日线冒充韩国正式收盘、用当前盘口代替历史事实或接入第二价格源。T-014 只加强确认链正式投递门；日线自然达到 50 根后再用独立任务冻结可真实验收的 Entry 公式。

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
go run ./cmd/catalyst-check
go run ./cmd/crowding-check
```

探针实时复核版本化映射、退市、OI、mark/oracle、双边盘口和 OI cap。429、超时、断线与 stale 的降级策略由确定性 fixture 校验；不以破坏公共服务或等待真实故障作为验收手段。

## 12. 结论

T-002 的数据源边界可冻结：六个核心指标存在统一 Hyperliquid 映射；非核心 USDKRW、DXY 和美债收益率当前不可用，依赖它们的阶段停止处理而不补源。T-008 已证明 SKHY 连续合约日线接口可用，但截至 2026-08-16 已完成历史仍少于 EMA50 门槛，Price Structure 和 Crowding price extension 必须暂时降级。T-010 已证明 DART 首次损失披露可与 Hyperliquid 双资产完整 24 小时窗口组成确定性 Catalyst，但该能力只覆盖一个报告类型和最近 5 个营业日 feed。T-012 已证明 funding、premium 和 20 日 volume 可用，但历史 OI 没有公共 endpoint；当前 Crowding 仍不足 4 项。所有可用性均须在运行时重新发现和过 Gate，本次快照不构成永久保证。

## 13. 官方参考

- [Hyperliquid Info endpoint](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/info-endpoint)
- [Hyperliquid Perpetuals API](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/info-endpoint/perpetuals)
- [Hyperliquid WebSocket subscriptions](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/websocket/subscriptions)
- [Hyperliquid rate limits](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/rate-limits-and-user-limits)
- [Hyperliquid historical data](https://hyperliquid.gitbook.io/hyperliquid-docs/historical-data)
- [Hyperliquid funding](https://hyperliquid.gitbook.io/hyperliquid-docs/trading/funding)
- [Hyperliquid WebSocket](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/websocket)
- [Hyperliquid WebSocket timeouts and heartbeats](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/websocket/timeouts-and-heartbeats)
- [trade.xyz Specification Index](https://docs.trade.xyz/consolidated-resources/specification-index)
- [trade.xyz Korea assets](https://docs.trade.xyz/asset-directory/korea)
- [trade.xyz Equity indices](https://docs.trade.xyz/xyz-perps-specification/equity-indices)
- [trade.xyz Terms of Use](https://trade.xyz/terms)
- [KRX Data Marketplace](https://data.krx.co.kr/contents/MDC/MAIN/main/index.cmd?locale=ko_KR)
- [KRX 投资者别净买入排名统计](https://data.krx.co.kr/contents/MDC/MDI/outerLoader/index.cmd?screenId=MDCSTAT024)
- [KRX OPEN API 服务清单](https://openapi.krx.co.kr/contents/OPP/INFO/service/OPPINFO004.cmd)
- [KRX OPEN API 使用方法](https://openapi.krx.co.kr/contents/OPP/INFO/OPPINFO003.jsp)
- [KRX OPEN API 使用条款](https://openapi.krx.co.kr/contents/OPP/INFO/OPPINFO002.jsp)
- [KRX 期货数据商品](https://data.krx.co.kr/contents/MDC/DATA/datasale/index.cmd?prodType=FF&viewNm=dataProdList)
- [KRX/Koscom 市场数据使用政策](https://data.krx.co.kr/inc/datasale/Market%20Data%20Usage%20Polices_ko.pdf)
- [DART RSS 服务说明](https://dart.fss.or.kr/introduction/content6.do)
- [OpenDART 使用条款](https://opendart.fss.or.kr/intro/terms.do)
- [SK hynix Newsroom Terms of Use](https://news.skhynix.com/en/terms-of-use/)
