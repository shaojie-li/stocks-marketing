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

## D-002 Hyperliquid 统一合约行情，外部来源只补分析事实

- 状态：Accepted
- 日期：2026-08-15
- 关联：[T-002](https://github.com/shaojie-li/stocks-marketing/issues/2)

### 背景

系统最终在 Hyperliquid 的币股合约上观察并可能执行交易，但全局分析还依赖 SOXX/QQQ、Samsung/KOSPI、韩国外资流、宏观 Consensus 和公司原始披露。2026-08-15 的公共 live check 只发现有非零 OI 的 NVDA、AMD、MU、SMH 和 SKHY 等部分合约；关键基准和事实数据并不都存在于 Hyperliquid。

若强制所有分析只使用 Hyperliquid，六个核心指标会因缺少基准长期不可用。若同时把外部现货价格和合约价格混成一个字段，交易时段、溢价和周末 oracle 又会被错误解释。

### 决策

- Hyperliquid 是币股合约的统一行情来源：合约发现、mark、oracle、L2、funding、OI、成交量、Crowding 和未来执行前检查均来自 Hyperliquid。
- 外部来源只补充 Hyperliquid 不具备的分析事实：现货/ETF/指数基准、韩国投资者资金流、官方宏观数据、市场 Consensus 和公司原始披露。
- 合约 Observation 与参考市场 Observation 分开保存，分别记录 symbol、venue、session、timestamp 和 source；不得用外部现货价覆盖 Hyperliquid mark，也不得把周末合约报价描述成底层现货开盘。
- Relative Strength 和 Alpha 使用参考市场中同窗口、同 session 语义的数据计算；Hyperliquid 用于验证可执行价格、溢价、流动性和拥挤度。
- Hyperliquid 合约通过 `dex + asset` 动态发现。名称命中后仍须检查非零 OI、双边盘口、oracle freshness 和 50 bps 深度；不得永久硬编码某个 DEX 或把零 OI 同名合约视为可交易。
- 外部分析数据缺失时对应指标明确 `UNAVAILABLE` 并降低 Confidence，不改用不等价的 Hyperliquid 合约凑数。
- 当前范围仍然只做 Discord 决策辅助；本决策定义未来执行数据边界，不授权自动下单。

### 影响

- 数据模型需要区分 `REFERENCE_MARKET` 与 `CONTRACT_MARKET`，报告同时展示参考市场状态和合约市场状态。
- Hyperliquid 是必需链路；外部供应商可以按单项能力替换，不进入交易执行路径。
- 同一分析可能出现“参考趋势有效，但当前合约流动性不可执行”，此时 Trend Score 保持独立，Entry Score 或执行资格下降。
- T-002 仍需验证外部最小组合，但不再追求让任一外部供应商同时承担当合约行情和交易价格源。
