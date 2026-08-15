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
