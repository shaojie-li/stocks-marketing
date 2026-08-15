---
name: repo-task-state
description: 核验仓库实施和交付所需的 Git、远端、GitHub Issue、PR 与 CI 状态。必须在新窗口或任务交接、开始工程任务前、以及准备合并 PR 前使用；只返回紧凑状态，不执行提交、推送、合并或关闭 Issue。
---

# 仓库任务状态核验

## 触发时机

在以下场景使用本 Skill，并把脚本输出作为动态事实来源：

1. 新窗口、切换任务或生成任务交接：使用 `--mode handoff`。
2. 开始规划、修改或评审工程变更：使用 `--mode preflight`。
3. 准备 PR 合并：使用 `--mode merge --pr <number>`。

默认使用 `--fetch` 获取远端最新引用。只有离线排查时才省略；省略后不得把本地已有远端引用描述为远端最新状态。

`--fetch` 依赖真实网络和本机 GitHub 认证。在启用网络沙盒的环境中，必须在具备外部网络权限的执行上下文运行；如果首次执行因 DNS、网络隔离或 GitHub 连接失败而返回 `fetch_status=failed`，应立即使用同一命令申请外部执行权限重试。外部重试成功前，不得把沙盒失败解释为远端、认证或仓库异常。

## 执行与解释

从仓库根目录执行：

```bash
python3 .agents/skills/repo-task-state/scripts/check-repo-state.py \
  --mode <handoff|preflight|merge> [--pr <number>] --fetch
```

脚本默认只读；`--fetch` 是唯一会更新本地远端引用的显式选项。脚本返回 JSON，优先阅读 `overall`、`git`、`github`、`ci` 和 `next_action`，不要把原始 API 响应复制到上下文。

脚本依赖本机已认证的 `gh` CLI。若 GitHub 状态为 `unknown`，在支持 GitHub Connector 的环境中可以用 Connector 补齐同一组字段，但仍必须报告认证/网络问题，不能把未知状态改写成通过。

状态处理规则：

- `pass`：可以把该项作为已核实事实报告。
- `attention`：继续前说明缺失项，不得包装成完全通过。
- `blocked`：暂停受影响的提交、PR 或合并动作，说明解除条件。
- `unknown`：网络、权限或工具不可用；不得猜测为通过。
- 只有 `head_sha_matches=true` 且 CI 对应结论为 `success`，才能把 PR head 的 CI 描述为通过。
- 没有开放 `status:next` 或目标 Issue 时，实施状态是 `attention`/`blocked`，但不阻止明确范围内的研究或文档设计。

本 Skill 不授权任何外部写操作。提交、推送、创建 PR、Ready、合并、关闭 Issue 和删除 worktree 必须继续遵循项目 Git 规范及用户授权。

## 资源

- `scripts/check-repo-state.py`：执行确定性状态检查并返回紧凑 JSON。
