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
```

T-001 至 T-005 已完成，形成规则契约、Hyperliquid 数据边界、可运行的最小垂直链路、带 Eligibility Gate 的真实行情快路径，以及可审计的同窗口收益和六个核心指标。下一任务应在这些确定性特征之上实现 Trend Score 状态机，不把状态迁移交给 LLM。

## 4. 稳定任务索引

| 里程碑 | 编号 | 任务 | GitHub | 状态 |
|---|---|---|---|---|
| M0 | T-001 | 定义全局分析规则与结构化输出契约 | [#1](https://github.com/shaojie-li/stocks-marketing/issues/1) | `DONE` |
| M0 | T-002 | 验证核心数据源的真实覆盖、时效与授权边界 | [#2](https://github.com/shaojie-li/stocks-marketing/issues/2) | `DONE` |
| M1 | T-003 | 建立项目骨架与最小可验证垂直链路 | [#3](https://github.com/shaojie-li/stocks-marketing/issues/3) | `DONE` |
| M2 | T-004 | 接入 Hyperliquid 实时行情快路径与数据质量门 | [#8](https://github.com/shaojie-li/stocks-marketing/issues/8) | `DONE` |
| M2 | T-005 | 计算同窗口收益与六个核心指标 | [#10](https://github.com/shaojie-li/stocks-marketing/issues/10) | `DONE` |

## 5. 暂不进入关键路径

- 自动交易、钱包签名、仓位管理和私有交易接口；
- Web 管理后台和移动 App；
- Redis、Kafka、独立向量数据库、微服务和 Kubernetes；
- 全球全部股票、新闻与产业数据源；
- 未验证授权的数据抓取或二次分发；
- 未冻结规则前的开盘、收盘、隔夜、周末和周一预期报告实现。
