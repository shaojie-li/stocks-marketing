---
name: create-issue
description: 按仓库的任务管理规范起草、校验并创建 GitHub Issue，处理 T- 编号、标题、标签、status:next、验收标准和质量门禁。用户显式输入 `$create-issue`，或已授权工作流明确要求创建一个范围和验收已确定的 Issue 作为必要下一步时触发；普通讨论、可选建议、远期 backlog、目标不完整或用户禁止创建时不触发。
---

# 标准化创建 Issue

## 目标

把用户需求转换成符合 `docs/TASK_MANAGEMENT.md` 的 GitHub Issue，并在实际创建成功后返回可核验的 URL。该 Skill 只创建任务，不自动创建分支、worktree 或开始实现。

本 Skill 可以由用户显式触发，也可以在当前已授权工作流明确要求“创建一个具体 Issue 是继续交付的必要下一步”时自动触发。自动触发只改变 Skill 选择，不扩大 GitHub 写入授权，也不能因普通讨论、可选建议、远期 backlog、目标或验收不完整、用户明确不希望建单而创建 Issue。所有创建、标签和状态变更都必须以 `gh` 的实际成功结果为准，不伪造 Issue 编号或 URL。

## 固定工具入口

在仓库根目录使用脚本完成确定性校验和 GitHub 操作：

```bash
python3 .agents/skills/create-issue/scripts/create_issue.py inspect --repo <owner/repo> --task-index-file docs/TASKS.md
python3 .agents/skills/create-issue/scripts/create_issue.py validate --title-file <title-file> --body-file <body-file> --kind plan
python3 .agents/skills/create-issue/scripts/create_issue.py create --repo <owner/repo> --task-index-file docs/TASKS.md --title-file <title-file> --body-file <body-file> --kind plan --label type:feature
```

脚本只使用 Python 标准库，并将 `gh` 的 stdout、stderr 和退出码保留为事实依据。

## 工作流

### 1. 读取规范与动态状态

1. 确认满足以下触发条件之一，并确定需求属于计划任务、Bug、安全、运维或设计：
   - 用户明确输入 `$create-issue`；
   - 当前工作流已获用户授权，且一个目标、范围和验收标准明确的 Issue 是继续该工作流的必要下一步，而不是可选建议。
   若只是普通需求讨论、实现请求中的可选拆分、远期 backlog、目标或验收不完整，或用户明确不希望创建 Issue，则不得自动触发。
2. 阅读 `AGENTS.md`、`docs/TASK_MANAGEMENT.md`、`docs/TASKS.md` 和必要的 `docs/DECISIONS.md`。
3. 使用 `$repo-task-state` 的 `preflight --fetch` 模式；GitHub 或认证状态 unknown 时不要声称已完成创建。
4. 运行 `inspect` 获取所有已分配的 `T-` 编号、开放的 `status:next` Issue 和可用标签。

### 2. 标题、编号和标签

计划任务使用：

```text
[M<里程碑>][T-<三位编号><可选大写子任务后缀>] <结果导向标题>
```

Bug、安全和运维分别使用 `[Bug]`、`[Security]` 和 `[Ops]` 前缀；不为临时任务分配 `T-` 编号。编号必须从 GitHub 全部 Issue 与本地任务索引中核对，不能复用，也不能仅根据当前对话猜测。

标签只使用项目已定义的标签：`status:next`、`status:blocked`、`type:design`、`type:feature`、`type:infra`、`type:bug`、`risk:security` 和 `risk:data-correctness`。同一时间只能有一个开放 Issue 使用 `status:next`；发现冲突时暂停，不自动移除已有标签。

### 3. 编写正文

按 [issue-template.md](references/issue-template.md) 生成正文，并至少填充以下内容：

- 目标和用户价值
- 依赖及解除条件
- 范围和非目标
- 可观察行为与验收标准
- 测试与质量门禁
- 适用的失败、重试、幂等、过期数据、安全和审计要求
- 初始完成记录，明确写“待实施”

验收标准必须是可检查的行为或产物，不能只写“完成开发”“测试通过”。涉及工程变更时，必须明确项目脚本、测试层级或质量门禁；涉及外部权限、凭据或服务时，写明阻塞条件和解除条件。

### 4. 校验与创建

1. 将标题和正文写入临时文件，运行 `validate`；校验失败先修正文案，不绕过校验。
2. 使用 `create` 调用 `gh issue create`。若 GitHub 标签尚未建立，默认阻止创建并报告缺少的标签；只有项目文档允许的兼容场景才显式使用 `--compatibility`，且不能跳过 `status:next` 的唯一性和标签检查。
3. 只有 `gh issue create` 成功并返回 URL 后，才报告 Issue 已创建；随后再次用 `gh issue view` 读取编号、标题、状态和标签确认结果。
4. 创建完成后不自动设置 `status:next`、不自动关闭旧 Issue、不创建分支、不启动实现。若用户明确在本次请求中要求 `status:next`，必须在创建前完成唯一性检查并作为创建参数传入。

## 暂停条件

需求缺少目标或验收标准、无法确定任务类型或里程碑、T- 编号有冲突、已有 `status:next`、标签不存在、GitHub fetch/认证失败、Issue 创建返回非零退出码，或用户要求创建重复任务时，暂停并报告具体缺口。

## 交付输出

最终报告必须包含：Issue URL 和编号、标题、类型标签、是否设置 `status:next`、正文中的验收标准摘要，以及未完成的后续动作。失败时只报告草稿文件和阻塞原因，不声称 Issue 已存在。
