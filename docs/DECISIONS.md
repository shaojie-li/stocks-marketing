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
