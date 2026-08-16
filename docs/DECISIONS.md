# 项目决策记录

## D-001 全局分析采用确定性规则引擎与受约束 AI 解释层

- 状态：Accepted
- 日期：2026-08-15
- 关联：[T-001](https://github.com/shaojie-li/stocks-marketing/issues/1)

### 背景

全局分析同时涉及跨市场收益率、交易时段、资金流、状态迁移、数据缺失和模型解释。若把公式、阈值和降级规则全部交给 Prompt，同一输入可能得到不同状态，也无法可靠审计旧报告。

### 决策

- 使用 [`GLOBAL_ANALYSIS_CONTRACT.md`](GLOBAL_ANALYSIS_CONTRACT.md) 作为版本化规则事实源；初始版本为 `global-analysis/1.0.0`。
- 时间窗口、六个指标、状态、三个 Score、Memory 状态迁移、数据质量和 Confidence 全部由确定性领域逻辑计算。
- AI 只接收已经冻结的 Evidence Bundle，输出必须符合 JSON Schema，并引用已有证据。
- 代理资产必须显示实际 symbol 和代理关系，字段和报告不得把代理称为原指数本身。
- SKHY Sector Alpha 与 SKHY Market Alpha 分开保存，不设置含义不明的合成 SKHY Alpha。
- 缺失和冲突降低覆盖率与 Confidence，不按零分或多空信号处理。
- 当前产品只提供 Discord 决策辅助，自动交易属于独立产品和安全范围。

### 影响

- 后续阶段报告可以复用同一规则，但阈值变化必须提升规则版本并回放测试向量。
- 实现需要保存 Observation、Evidence、规则版本、输入哈希和状态迁移历史。
- 模型无法通过改变措辞覆盖领域引擎状态；这降低灵活性，但换取可重复、可测试和可追溯。
- 数据源验证必须证明能够提供本契约要求的 session、时间戳、资金流和 Crowding 输入；无法提供时明确降级或调整 MVP。

## D-002 Hyperliquid 是唯一市场数据源，缺失时停止处理

- 状态：Accepted
- 日期：2026-08-15
- 关联：[T-002](https://github.com/shaojie-li/stocks-marketing/issues/2)

### 背景

系统最终在 Hyperliquid 的币股合约上观察并可能执行交易。若行情同时来自多个供应商，需要处理 symbol 映射、交易时段、价格口径、授权和故障切换，会显著增加早期产品的错误面。

2026-08-15 的首次公共 live check 只按字面 symbol 查找，错误地把 trade.xyz 别名判为缺失。复核确认 `KR200`、`SOXL`、`XYZ100`、`SMSN`、`SP500`、`BRENTOIL` 和 `CL` 均存在且有非零 OI；`KRW` 与 `DXY` 虽可发现，但已退市且 OI 为零。该错误表明业务语义不能通过字符串相等解析，发现也不能直接等同于可用，必须维护版本化映射并实时执行 Eligibility Gate。

### 决策

- Hyperliquid 是唯一市场数据源。合约发现、mark、oracle、L2、funding、OI、成交量、Crowding 和未来执行前检查均来自 Hyperliquid。
- 不接入 KIS、Massive 或其他来源补齐市场行情；新闻文章中的价格也不得使用。非市场事实源另行决策，不能提供或覆盖价格字段。
- 每个分析阶段先用版本化映射解析业务资产，再执行确定性 Eligibility Gate。只有该阶段全部必需价格 symbol、字段、窗口和 freshness 均可用时，才创建 Analysis Run、调用 AI 和发送 Discord。
- Gate 失败时记录结构化 `SKIPPED_SOURCE_INCOMPLETE` 运维事件，包含缺失项和检查时间；不生成残缺交易报告，不把缺失按零分或中性信号处理。
- 周末 Hyperliquid 合约状态可以是 `CONTINUOUS`，不得描述成底层美韩现货市场 OPEN。
- Hyperliquid 合约通过 `dex + asset` 动态发现。名称命中后仍须检查非零 OI、双边盘口、oracle freshness 和 50 bps 深度；不得永久硬编码某个 DEX 或把零 OI 同名合约视为可交易。
- 当前范围仍然只做 Discord 决策辅助；本决策定义未来执行数据边界，不授权自动下单。
- 只持久化有界聚合、异常和审计证据，Discord 只展示派生分析。原始行情二次分发授权未确认，在取得书面授权或法律复核前禁止提供 raw data feed。

### 影响

- T-002 不再验证 KIS、Massive 或宏观数据商，改为冻结 Hyperliquid 覆盖矩阵、失败行为和 Eligibility Gate 输入。
- `global-analysis/1.1.0` 使用 `SMH`、`XYZ100`、`SMSN` 和 `KR200` 分别承载 Semiconductor、Growth、Samsung 和 Korea 200 语义，并在报告中公开代理关系。
- `SOXL` 是每日重置的 3 倍杠杆 ETF，不能直接代替 SOXX；当前选择非杠杆 `SMH`，避免系统性放大 Relative Strength。
- 新合约上线后通过动态发现自动进入候选，但仍须完整通过字段、时间和流动性检查。
- 公司公告和产业新闻不属于市场行情；是否接入官方事实源由后续独立决策处理，不能提供或覆盖任何价格字段。

## D-003 业务配置统一存入数据库，敏感值认证加密

- 状态：Accepted
- 日期：2026-08-15
- 关联：[T-003](https://github.com/shaojie-li/stocks-marketing/issues/3)

### 背景

应用后续会增加 OpenAI、Discord、报告调度和关注标的等配置。若把它们分散在本地密码 App、配置文件和环境变量中，部署、审计和修改路径会变得不一致。另一方面，把数据库密码或解密主密钥放回同一个数据库会形成无法启动的循环，并使数据库泄露时的加密失去意义。

### 决策

- 所有业务配置统一保存到单表 `app_settings`，使用唯一 `setting_key` 和显式版本号，不建立多套配置系统。
- OpenAI Key、Discord Webhook 等敏感值使用 Go 标准库 AES-256-GCM 认证加密，setting key 作为 associated data，数据库只保存带版本前缀的密文。
- 只保留 `DATABASE_URL` 和 `SETTINGS_MASTER_KEY` 两个部署启动凭据。它们由运行环境注入，不保存到 `app_settings`，也不进入本地密码 App、仓库、日志或错误信息。
- 配置命令从标准输入接收值；不提供批量明文列出或导出全部敏感配置的接口。
- 第一阶段不引入 Vault、云 KMS 或单独配置服务。需要多人权限、自动轮换或合规审计时再扩展密钥管理。

### 影响

- 丢失 `SETTINGS_MASTER_KEY` 会导致数据库中的敏感配置不可恢复，因此生产环境必须在部署平台安全保存并稳定注入该值。
- 数据库备份不会包含可直接使用的敏感明文；恢复服务同时需要数据库备份和对应主密钥。
- 业务配置变更可以集中审计；主密钥轮换工具尚未实现，在出现实际轮换要求前不提前增加复杂度。

## D-004 Analysis Run 聚合确定性 Bundle 与可比历史

- 状态：Accepted
- 日期：2026-08-15
- 关联：[T-007](https://github.com/shaojie-li/stocks-marketing/issues/14)

### 背景

T-003 的 `analysis_runs` 只按完整 report 哈希去重，能够支持最小 Discord 垂直链路，但不能表达全局契约规定的稳定分析身份，也不能可靠查询可比 Score 或恢复 Memory 多日状态。若由调用者传入“上一结果”，并发重试、乱序 session 或组件覆盖集合变化会产生不可审计的方向和状态分叉。

### 决策

- 继续使用 `analysis_runs` 作为唯一 Analysis Run 聚合根，不建立平行运行表。迁移前的 report-only 行和 delivery 外键保持可读；新路径保存稳定身份、输入哈希、核心指标和不可变确定性 Bundle。
- Bundle JSONB 保存完整冻结产物；稳定身份、Score 可比字段、权威分值和 Memory session 字段关系化保存。权威 Score 使用 PostgreSQL `NUMERIC`，不使用 `float64`。
- Trend 前值只从数据库选择主资产、规则版本、phase、window type 和可用组件集合完全相同的更早结果，调用者提供的前值不参与持久化路径。
- Memory 以 `rule_version + primary_asset` 作为状态流、以 session date 作为自然历史身份。每个 session 的 from/to、分类、streak、原因、Confidence 和证据不可变；精确重放复用历史，冲突和旧 session 回插显式失败。
- Analysis Run、Score 和 Memory 在一个事务中写入。按稳定身份锁和 Memory 状态流锁的固定顺序串行化并发提交，避免重复版本、丢失更新、分叉和死锁。

### 影响

- 后续 AI 和 Discord 报告只需读取已冻结 Bundle，不再计算权威指标、Score 方向或 Memory 状态。
- JSONB 保留完整可回放内容，关系型列承担幂等、排序和可比查询；新增查询字段时必须先证明真实消费路径，避免复制整份 Bundle 到多套表。
- 当前 Price Structure 与 Foreign Flow 仍为 `UNAVAILABLE`。Trend 可以按 80% 覆盖率计算，Memory 记录 `DATA_UNAVAILABLE` 并保持前态；本决策不授权第二行情源或价格反推资金流。

## D-005 Price Structure 使用 Hyperliquid 已完成 UTC 连续合约日线

- 状态：Accepted
- 日期：2026-08-15
- 关联：[T-008](https://github.com/shaojie-li/stocks-marketing/issues/16)

### 背景

全局契约原本要求“正式收盘”和“已确认 20 日 swing low”，但 Hyperliquid `xyz:SKHY` 是连续合约，不能把其价格冒充韩国现货正式收盘；“已确认 swing low”也没有可执行定义。2026-08-15 的公共 live check 证明 `1d` candle 可读，但固定 `as_of` 前只有 37 根已完成日线，不足 EMA50 门槛。

### 决策

- Trend 的 SKHY Price Structure 明确描述 Hyperliquid `xyz:SKHY` UTC 连续合约日线，不写入传统市场 Regular Close，也不声称韩国现货市场处于 OPEN/CLOSED。
- 只使用 close time 严格早于分析 `as_of` 的连续 `1d` candle；形成中、间断、重复、乱序或非法载荷均显式不可用。
- 至少 50 根已完成日线后计算 EMA20、EMA50、Wilder ATR14，以及评估日前 20 根日线的最低 low。后者不包含评估日，避免把当日新低同时当成已经确认的支撑。
- Price Structure 及其窗口、完成日线数、精确十进制派生值和响应哈希证据进入 Analysis Bundle 输入哈希。可用时 Trend 覆盖率为 90%，不足时保持 80%；两者 Confidence 上限均为 `MEDIUM`。
- 规则版本提升为 `global-analysis/1.2.0`。不新增关系型 Price Structure 历史表，继续由不可变 Bundle 和现有 Score 历史承担审计与可比查询。

### 影响

- 当前真实历史不足时 Price Structure 仍为 `UNAVAILABLE / INSUFFICIENT_HISTORY`；达到 50 根后无需代码或配置变更即可自动启用。
- 80% 与 90% 的 Trend 组件集合不可直接比较，组件集合变化后的首次结果不产生方向。
- Foreign Flow 和 Catalyst 不因价格结构可用而被补齐；任一关键输入缺失时 Memory 仍为 `DATA_UNAVAILABLE`。
- `HEALTHY_PULLBACK_WITH_BID` 等依赖正式交易时段的 Entry 规则不在本决策中实现。

## D-006 当前不接入 KRX Foreign Flow

- 状态：Accepted
- 日期：2026-08-15
- 关联：[T-009](https://github.com/shaojie-li/stocks-marketing/issues/18)

### 背景

全局契约需要分别保存 SK Hynix 个股、KOSPI 市场和 KOSPI 200 Futures 的外资净买卖，并用同一范围、同一交易日、同一口径的净买额和成交额计算 `flow_ratio_pct`。价格、成交量、OI 或持股量变化都不能替代真实投资者分类数据。

KRX Data Marketplace 网页展示股票和衍生品投资者交易实绩，KRX 也出售期货投资者类型别日度交易数据，但公开 OPEN API 服务清单没有投资者类别净买卖接口。现行 OPEN API 条款只允许非商业用途并禁止向第三方提供数据；付费市场数据的非查询型加工和外部提供则需要单独合同、用途申报、审批和可能的费用。当前项目没有覆盖后台自动获取、派生分析和 Discord 输出的书面授权。

### 决策

- T-009 结论为 `NO-GO`，当前不接入 KRX 或其他来源的 Foreign Flow，不申请无关 API 密钥，不购买数据商品，也不调用未文档化网页接口。
- SK Hynix 个股、KOSPI 市场、KOSPI 200 Futures 净买卖和外资 OI 变化全部保持 `UNAVAILABLE`；缺失不补零、不混用范围，也不由价格、成交量、funding、全市场 OI 或持股量推断。
- Hyperliquid 继续是唯一价格行情源。`000660` 与 `xyz:SKHY` 只允许建立事实映射，韩国现货数据不得提供或覆盖 Hyperliquid 价格字段。
- 不为不可用能力创建客户端、Schema、配置、任务、重试或凭据流程。Trend 继续按可用权重归一化，Memory 按既有契约保持 `DATA_UNAVAILABLE`。
- 不用 AI 浏览器、截图识别或 LLM 网页分析绕过数据授权和确定性边界。用户手动提供的单次页面证据只能用于非权威解释，不能进入 Analysis Bundle、Trend Score、Memory 或生产历史。
- 只有取得 KRX/Koscom 书面许可，且许可明确覆盖三个范围、目标字段、最终性/修订语义、后台自动获取、派生计算、必要存储和 Discord 输出，才允许创建新的可行性评审 Issue；不因“网页可见”或“商品可购买”自动解除本决策。

### 影响

- 当前系统不会计算或展示伪 Foreign Flow，代价是 Trend 覆盖率暂时不能提升到 100%，Memory 也不会因该输入缺失而迁移。
- 本结论不是断言 KRX 没有数据，而是确认当前项目没有同时满足字段覆盖、稳定自动化和用途授权的路径。
- 若未来商业价值足以承担合同、费用和合规义务，必须先把许可作为产品决策处理，再评估工程实现；在此之前优先推进不依赖未授权数据的 M2 能力。

## D-007 Catalyst 首个事实源使用 DART 公司 RSS

- 状态：Accepted
- 日期：2026-08-15
- 关联：[T-010](https://github.com/shaojie-li/stocks-marketing/issues/20)

### 背景

全局契约已定义 Catalyst 价格接受阈值，但没有冻结可自动获取且方向可确定的官方事件源、事件范围和价格窗口。SK hynix Newsroom 虽提供 RSS，其现行 Terms 只允许非商业使用，并禁止 robot、spider 或其他自动设备监控或复制材料，不能作为后台生产来源。DART 官方公司 RSS 面向自动更新场景，公开提供上市公司最近 5 个营业日披露；2026-08-15 live check 已验证 SK hynix 披露及精确时间字段。

### 决策

- 首个 Catalyst 唯一事实源为 DART SK hynix 公司 RSS，corp code 固定为 `00164779`。SK hynix Newsroom、媒体、搜索、网页识别和 LLM 分类不作为回退。
- 只识别报告名精确为 `파생상품거래손실발생` 的首次披露，映射为 `DERIVATIVE_TRADING_LOSS_OCCURRED / CONFIRMED / BEARISH`。其他报告不产生方向；更正、补充或撤回显式冲突。
- `rcpNo` 是稳定事件 ID；`pubDate` 与 `dc:date` 必须一致并同时作为 published/event time。只保存必要元数据、响应哈希和派生结果，不保存披露正文或再分发原始 feed。
- 价格继续只来自 Hyperliquid。目标 `xyz:SKHY` 与基准 `xyz:SMSN` 使用同一事件前分钟和同一 24 小时评估分钟；任一侧不完整则 Catalyst 不可用。
- 事件选择、方向、精确十进制收益与 `ACCEPTED / NEUTRAL / REJECTED` 全部由 Go 领域逻辑计算，进入 Analysis Bundle 输入哈希。LLM 不参与权威判断。
- 规则版本提升为 `global-analysis/1.3.0`。本切片不新增后台轮询、事件表、历史回补、Fundamental、Entry、AI 或 Discord 链路。

### 影响

- 系统能够审计一个真实官方损失披露的市场接受，但不声称覆盖全部公司 Catalyst；事件离开 DART 最近 5 个营业日窗口后，只读检查会返回没有最近事件。
- Catalyst 可用不会改变 Trend Score。Foreign Flow 仍不可用，因此 Memory 保持 `DATA_UNAVAILABLE`、前态不变、streak 清零且 Confidence 上限为 `LOW`。
- 新增下一种事件前，必须单独证明官方来源、自动化用途边界、精确方向语义、修订行为和可测试价格窗口，不能把任意新闻分类扩展进来。

## D-008 降级全局分析使用非入场安全门

- 状态：Accepted
- 日期：2026-08-15
- 关联：[T-011](https://github.com/shaojie-li/stocks-marketing/issues/22)

### 背景

真实 live check 已能计算同窗口核心指标、Trend Score 和 DART Catalyst，但 Fundamental、Crowding、Foreign Flow、完整 Entry、正式交易时段结构和客观盈亏比仍不可用。若等待所有组件完成后才生成报告，无法提前验证分析价值；若直接把缺失组件交给模型补齐，又会把局部趋势强势包装成不可审计的交易建议。现有 Discord 摘要还会把 JSON `null` Entry 解码为 Go `float64` 零值，存在降级报告被错误当作正式结果发送的风险。

### 决策

- 价格 Gate 通过时允许生成真实只读 shadow 分析，缺失的非价格组件保持 `null/UNAVAILABLE`，不补零、不推断方向。
- Entry 覆盖率低于 70% 或分值为 `null` 时，策略固定为 `OBSERVE/WAIT + NO_TRADE`，路由固定为 `SHADOW_ONLY`。不输出目标价、止损价、仓位或盈亏比，不进入正式 Discord 业务投递。
- `NO_TRADE` 不伪造交易失效位；任何非 `NO_TRADE` 结构必须提供至少一个带证据引用的客观失效条件。
- Catalyst Entry 贡献必须结合事件方向和拟评估交易方向：接受事件方向只支持同向，拒绝事件方向只支持反向，`NEUTRAL` 为中性；没有有效交易方向时该组件不可用。
- 正式提交边界在数据库、River 和 Discord 副作用之前执行同一确定性安全校验。模型不能改变安全结果或路由。
- 规则版本提升为 `global-analysis/1.4.0`。本切片不实现 Crowding、Fundamental、完整 Entry、模型调用、正式调度或生产部署。

### 影响

- 系统可以更早用真实数据验证报告价值，同时保证数据不足只产生观察结论。
- 降级报告与正式报告共享 Schema 和领域事实，不建立第二套分析规则；`NO_ENTRY` 只作为内部安全决策，机器报告继续复用 `OBSERVE + NO_TRADE`。
- 后续 T-012 可以独立补 Crowding，提高 Entry 覆盖率，但不能绕过客观失效条件和正式投递门。
