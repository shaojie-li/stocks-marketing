# 实施路线与任务索引

状态：Active v1.0
更新日期：2026-08-15

GitHub Issues 是实施任务的唯一事实源。本文件只保存稳定编号、依赖顺序和 Issue 索引；任务正文规范见 [任务管理规范](TASK_MANAGEMENT.md)。

## 1. 产品边界

当前阶段提供可追溯的市场分析和 Discord 决策辅助，不连接钱包、券商私有交易接口或自动下单。自动交易、仓位管理和交易风控必须作为后续独立范围设计和授权。

## 2. 排序原则

1. 数据正确性、安全和可追溯高于功能速度。
2. 先验证关键数据源和规则，再建立工程链路。
3. 确定性计算、状态迁移和质量判断不交给 LLM。
4. 垂直闭环高于一次铺开全部市场、新闻源和报告阶段。
5. 只有范围、依赖和验收明确的近期任务才创建 Issue。

## 3. 当前关键路径

```text
T-001 全局分析规则与输出契约 ─┐
                                ├→ T-003 项目骨架与最小垂直链路（DONE）
T-002 核心数据源真实能力验证 ─┘
                                      ↓
T-004 Hyperliquid 实时行情快路径与数据质量门（DONE）
                                      ↓
T-005 同窗口收益与六个核心指标（DONE）
                                      ↓
T-006 Trend Score 与 Memory 状态迁移（DONE）
                                      ↓
T-007 确定性 Analysis Bundle 与可比历史（DONE）
                                      ↓
T-008 Hyperliquid SKHY 日线价格结构与历史充足性门（DONE）
                                      ↓
T-009 官方 Foreign Flow 覆盖与授权验证（DONE / NO-GO）
                                      ↓
T-010 DART 损失披露 Catalyst 的 24h 价格接受（DONE）
                                      ↓
T-011 真实降级全局分析与非入场安全门（DONE）
                                      ↓
T-012 Hyperliquid SKHY Crowding 与历史充足性门（IN PROGRESS）
```

T-001 至 T-011 已完成，形成规则契约、Hyperliquid 数据边界、可运行的最小垂直链路、带 Eligibility Gate 的真实行情快路径、可审计的同窗口指标、确定性的 Trend Score 和 Memory 状态迁移、不可变 Analysis Bundle 与可比历史、可在历史充足后自动启用的 SKHY 连续合约日线价格结构、DART 官方损失披露的 24 小时价格接受，以及真实降级分析的非入场安全门。T-009 对 KRX Foreign Flow 作出 `NO-GO` 决策：当前没有同时满足字段覆盖、稳定自动化和 Discord 输出授权的数据路径，因此不实施。

## 4. 稳定任务索引

| 里程碑 | 编号 | 任务 | GitHub | 状态 |
|---|---|---|---|---|
| M0 | T-001 | 定义全局分析规则与结构化输出契约 | [#1](https://github.com/shaojie-li/stocks-marketing/issues/1) | `DONE` |
| M0 | T-002 | 验证核心数据源的真实覆盖、时效与授权边界 | [#2](https://github.com/shaojie-li/stocks-marketing/issues/2) | `DONE` |
| M1 | T-003 | 建立项目骨架与最小可验证垂直链路 | [#3](https://github.com/shaojie-li/stocks-marketing/issues/3) | `DONE` |
| M2 | T-004 | 接入 Hyperliquid 实时行情快路径与数据质量门 | [#8](https://github.com/shaojie-li/stocks-marketing/issues/8) | `DONE` |
| M2 | T-005 | 计算同窗口收益与六个核心指标 | [#10](https://github.com/shaojie-li/stocks-marketing/issues/10) | `DONE` |
| M2 | T-006 | 计算 Trend Score 并执行 Memory 状态迁移 | [#12](https://github.com/shaojie-li/stocks-marketing/issues/12) | `DONE` |
| M2 | T-007 | 组装确定性 Analysis Bundle 并持久化可比 Score 与 Memory 历史 | [#14](https://github.com/shaojie-li/stocks-marketing/issues/14) | `DONE` |
| M2 | T-008 | 接入 Hyperliquid SKHY 日线价格结构及历史充足性门 | [#16](https://github.com/shaojie-li/stocks-marketing/issues/16) | `DONE` |
| M2 | T-009 | 验证官方 Foreign Flow 数据覆盖与授权边界 | [#18](https://github.com/shaojie-li/stocks-marketing/issues/18) | `DONE / NO-GO` |
| M2 | T-010 | 评估 DART 损失披露 Catalyst 的 24h 价格接受 | [#20](https://github.com/shaojie-li/stocks-marketing/issues/20) | `DONE` |
| M2 | T-011 | 建立真实降级全局分析与非入场安全门 | [#22](https://github.com/shaojie-li/stocks-marketing/issues/22) | `DONE` |
| M2 | T-012 | 计算 Hyperliquid SKHY Crowding 并显式降级不足历史 | [#24](https://github.com/shaojie-li/stocks-marketing/issues/24) | `IN PROGRESS` |

## 5. 暂不进入关键路径

- Foreign Flow 生产接入：当前按 [D-006](DECISIONS.md#d-006-当前不接入-krx-foreign-flow) 为 `NO-GO`；只有取得明确覆盖 SK Hynix 个股、KOSPI 市场、KOSPI 200 Futures、后台自动获取、派生计算、必要存储和 Discord 输出的书面数据许可后，才重新评审并创建实施 Issue；
- 自动交易、钱包签名、仓位管理和私有交易接口；
- Web 管理后台和移动 App；
- Redis、Kafka、独立向量数据库、微服务和 Kubernetes；
- 全球全部股票、新闻与产业数据源；
- 未验证授权的数据抓取或二次分发；
- 未冻结规则前的开盘、收盘、隔夜、周末和周一预期报告实现。
