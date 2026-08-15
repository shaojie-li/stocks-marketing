# 项目决策记录

## D-001 全局分析采用确定性规则引擎与受约束 AI 解释层

- 状态：Accepted
- 日期：2026-08-15
- 关联：[T-001](https://github.com/shaojie-li/stocks-marketing/issues/1)

### 背景

全局分析同时涉及跨市场收益率、交易时段、资金流、状态迁移、数据缺失和模型解释。若把公式、阈值和降级规则全部交给 Prompt，同一输入可能得到不同状态，也无法可靠审计旧报告。

### 决策

- 使用 [`GLOBAL_ANALYSIS_CONTRACT.md`](GLOBAL_ANALYSIS_CONTRACT.md) 作为 `global-analysis/1.0.0` 的规则事实源。
- 时间窗口、六个指标、状态、三个 Score、Memory 状态迁移、数据质量和 Confidence 全部由确定性领域逻辑计算。
- AI 只接收已经冻结的 Evidence Bundle，输出必须符合 JSON Schema，并引用已有证据。
- QQQ 在 v1 中作为 Nasdaq 的可交易代理，字段和报告不得称其为 Nasdaq 指数本身。
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

2026-08-15 的公共 live check 发现有非零 OI 的 NVDA、AMD、MU、SMH 和 SKHY 等部分合约，但 SOXX、QQQ、Samsung、KOSPI 等关键基准不存在。项目接受覆盖不完整换取单一口径，缺失时不采用替代行情源。

### 决策

- Hyperliquid 是唯一市场数据源。合约发现、mark、oracle、L2、funding、OI、成交量、Crowding 和未来执行前检查均来自 Hyperliquid。
- 不接入 KIS、Massive、BLS 或其他来源补齐行情、指数、资金流或宏观数值；新闻文章中的价格也不得使用。
- 每个分析阶段先执行确定性 Eligibility Gate。只有该阶段全部必需 symbol、字段、窗口和 freshness 均可用时，才创建 Analysis Run、调用 AI 和发送 Discord。
- Gate 失败时记录结构化 `SKIPPED_SOURCE_INCOMPLETE` 运维事件，包含缺失项和检查时间；不生成残缺交易报告，不把缺失按零分或中性信号处理。
- 周末 Hyperliquid 合约状态可以是 `CONTINUOUS`，不得描述成底层美韩现货市场 OPEN。
- Hyperliquid 合约通过 `dex + asset` 动态发现。名称命中后仍须检查非零 OI、双边盘口、oracle freshness 和 50 bps 深度；不得永久硬编码某个 DEX 或把零 OI 同名合约视为可交易。
- 当前范围仍然只做 Discord 决策辅助；本决策定义未来执行数据边界，不授权自动下单。

### 影响

- T-002 不再验证 KIS、Massive 或宏观数据商，改为冻结 Hyperliquid 覆盖矩阵、失败行为和 Eligibility Gate 输入。
- 当前 SOXX、QQQ、Samsung、KOSPI 等缺失，因此依赖这些标的的原版全局分析会被 Gate 跳过，不发送交易报告。
- 新合约上线后通过动态发现自动进入候选，但仍须完整通过字段、时间和流动性检查。
- 公司公告和产业新闻不属于市场行情；是否接入官方事实源由后续独立决策处理，不能提供或覆盖任何价格字段。
