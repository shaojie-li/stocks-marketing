---
name: audit-production-pushes
description: 只读查询并审计 stock-market-monitoring 生产环境最近的 Discord 业务新闻推送。用于用户询问最近推送、消息重复、事实快报价值、Analysis 降级或失败、OpenAI Attempt、投递幂等、旧新闻重发，或需要把运维告警与业务推送分开诊断时。
---

# 审计生产推送

使用随附脚本取得脱敏、可复现的生产事实，分析根因，再给出结论。不得凭 Discord 截图猜测数据库状态。

## 查询

从仓库根目录执行：

```bash
./.agents/skills/audit-production-pushes/scripts/query_recent_pushes.sh --limit 10
```

需要不同 SSH 别名时增加 `--host <alias>`。脚本只执行 PostgreSQL `READ ONLY` 事务，默认连接 `aliyun-esc-singapore`，查询 `/opt/stock-market-monitoring` 的生产 Compose；失败必须非零退出。

脚本不返回原文、Webhook、凭据、模型原始输出、错误详情或完整内部 UUID。不得绕过这些边界，也不得为诊断执行重放、重试、反馈或数据库写入。

## 分析顺序

1. 先区分运维通知与业务新闻：`FIRING/RESOLVED` 属于 Alertmanager 运维通知；脚本返回的是成功业务 Delivery。
2. 检查 `summary`：最近 N 条中的文档数、核心事实数、重复行数和 Analysis 状态分布。
3. 逐条核对 `same_document_successes`、`same_core_fact_successes` 与 `same_event_successes`。同一文档和同一核心事实再次成功投递就是用户可见重复；新的 EventVersion 或 `content_hash` 不能单独证明事实有变化。
4. 对比 `source_version`、`event_version` 与 `page_views_last_updated`。只有页面浏览量等动态元数据变化而核心事实未变时，判定为版本膨胀，不是新闻更新。
5. 检查发布时间、首次发现和投递时间。旧文档反复实时推送属于时效性问题，即使来源和 Delivery 都真实成功。
6. 检查 Analysis 状态、降级原因、Attempt 错误码和成本。事实快报成功不证明方向价值；失败或降级样本不能进入 T-021B 方向分母。

## 输出结论

按严重度给出：

- 是否存在真实重复及数量证据；
- 根因位于来源版本、事件版本、质量门、模型调用还是 Discord 投递；
- 用户影响，包括通知疲劳、旧闻误判和评估分母污染风险；
- 已正常工作的边界，例如 Delivery 幂等是否在单个业务身份内收敛；
- 最小修复入口与验证条件。

诊断请求不授权修改生产或代码。用户明确要求修复时，再使用 `$ai-market-monitor-engineering` 按 TDD 实施。
