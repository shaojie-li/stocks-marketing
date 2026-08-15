---
name: worktree-delivery
description: 通过 Git worktree 从 origin/main 创建隔离分支，完成指定 GitHub Issue 的实现、验证、提交、推送、PR 合并和合并后清理。仅在用户明确输入 `$worktree-delivery` 时触发；适用于用户要求并行开发并完整交付到 main 的任务。
---

# Worktree 交付

## 目标

将一个有明确 Issue 事实源的工程任务放入独立 Git worktree，完成从 `origin/main` 到 `main` 的可追溯交付。手动触发本 Skill 代表用户授权本任务范围内的 commit、push、PR、squash merge 和合并后清理；不授权绕过保护规则、强推、降低质量门禁或扩大任务范围。

本 Skill 必须显式触发，不能因为普通的“实现功能”请求而隐式调用。没有 Issue 时暂停并提示先使用 `$create-issue`，不要自动创建 Issue。

## 固定工具入口

从仓库根目录执行脚本，减少重复的 Git 命令和状态解析：

```bash
python3 .agents/skills/worktree-delivery/scripts/worktree_delivery.py preflight --repo <repo>
python3 .agents/skills/worktree-delivery/scripts/worktree_delivery.py create --repo <repo> --path <absolute-worktree> --branch <branch>
python3 .agents/skills/worktree-delivery/scripts/worktree_delivery.py verify --repo <repo> --path <absolute-worktree>
python3 .agents/skills/worktree-delivery/scripts/worktree_delivery.py commit --repo <worktree> --message '<规范提交标题>' --file <path> [--file <path>]
python3 .agents/skills/worktree-delivery/scripts/worktree_delivery.py push --repo <worktree> --branch <branch>
python3 .agents/skills/worktree-delivery/scripts/worktree_delivery.py create-pr --repo <worktree> --branch <branch> --title '<PR标题>' --body-file <file>
python3 .agents/skills/worktree-delivery/scripts/worktree_delivery.py merge-and-cleanup --repo <repo> --path <absolute-worktree> --branch <branch> --pr <number>
```

脚本只执行参数明确的目标，不接受模糊路径或递归删除；实际实现和质量判断仍由 Codex 完成。

## 工作流

### 1. 前置核验

1. 确认用户明确使用 `$worktree-delivery`，并获得 Issue 编号或 URL。
2. 阅读仓库根目录 `AGENTS.md`、项目工程 Skill、`docs/GIT_COMMIT_CONVENTION.md` 和 Issue 正文。
3. 使用 `$repo-task-state` 的 `preflight --fetch` 模式核验远端、Issue 和当前工作区。
4. 运行 `preflight` 脚本刷新 `origin/main`，记录其 SHA；fetch、认证或 GitHub 状态未知时暂停。
5. 保留当前 worktree 的所有用户修改。当前 worktree 不干净不阻止新 worktree 创建，但绝不能把这些修改复制、暂存或提交到新任务。

### 2. 创建隔离 worktree

1. 根据 Issue 类型选择分支前缀，遵循 `<type>/<work-item>-<english-slug>`；没有稳定任务编号时使用 `issue<number>`，例如 `feat/issue34-workflow-skills`。
2. 使用脚本从 `origin/main` 创建新分支和 worktree；路径必须是绝对路径，且不能是现有 worktree、仓库根目录或含有未知文件的目录。
3. 使用 `verify` 确认新 worktree HEAD 与 `origin/main` SHA 相同，并确认分支没有已有提交。
4. 后续所有实现、测试、commit 和 push 命令都在新 worktree 中执行。

### 3. 实现与验证

1. 以 Issue 的范围、非目标和验收标准为边界；修改行为、架构、数据或操作方式时同步更新文档和 `docs/DECISIONS.md`。
2. 对本项目工程变更加载 `$ai-market-monitor-engineering`，执行其适用的 TDD 和质量门禁。
3. 先运行聚焦验证，再运行项目开发门禁；不要删除测试、降低断言或保留故意失败的 red 测试。
4. 检查 `git diff`、`git diff --check` 和待提交文件，排除密钥、生成垃圾和无关修改。

### 4. 提交和创建 PR

1. 提交标题遵循 `<type>(<scope>): <中文简述>`；每个提交只包含一个逻辑变化。
2. 先审查差异，再用 `commit` 脚本只暂存任务文件；脚本执行 `git diff --check` 和 staged diff 检查。
3. 用 `push` 脚本推送分支；推送前确认分支确实从 `origin/main` 开始且工作区只包含任务修改。

4. 用 `create-pr` 脚本创建 PR；标题使用与最终 squash commit 相同的格式，正文必须包含目标、范围、验收结果、实际门禁、剩余风险，并在适用时写 `Closes #<issue>`。
5. 记录 PR 编号和 head SHA。PR 创建失败、权限不足或远端状态未知时保留 worktree 并暂停。

### 5. 合并和清理

1. 等待或检查必需 CI、review 和合并条件。用有界检查推进；CI pending、失败、requested changes、冲突或 head SHA 变化时暂停并报告，不无限轮询。
2. 合并前必须再次使用 `$repo-task-state` 的 `merge --pr <number>` 模式，并确认 PR head SHA、CI、review 和 mergeable 状态均满足项目规范。
3. 通过脚本执行 squash merge；脚本会再次核验 PR 基线为 `main`、状态为 open、全部已报告检查通过，并在合并后确认 PR 状态为 merged。若上次合并已成功但清理中断，脚本允许从 `merged` 状态恢复清理。
4. 合并确认后脚本才可以删除本次创建的 worktree 和本地分支。清理失败时保留可恢复信息，不使用 `--force` 删除含有未提交修改的 worktree。
5. 最后 fetch `origin/main`，确认合并结果已进入远端 main，并输出 PR URL、merge commit、worktree 路径和清理状态。

## 暂停条件与恢复

以下情况必须暂停，而不是猜测或绕过：Issue 不明确、基线 fetch 失败、分支或路径冲突、门禁失败、CI 未通过、review 要求修改、合并冲突、权限不足、head SHA 改变或 cleanup 目标无法精确确认。

合并前暂停时保留 worktree 和分支，输出下一条可执行的恢复命令；合并后只要 PR 已确认 merged，就可以单独重试 `merge-and-cleanup`。不得因“看起来已经完成”提前删除 worktree。

## 交付输出

最终报告必须包含：Issue、分支、worktree 路径、PR URL、合并状态、实际运行的门禁、未执行门禁、剩余风险，以及是否已删除 worktree。若没有完成合并和清理，明确写出阻塞条件和下一步。
