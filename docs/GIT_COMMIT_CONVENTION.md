# Git 分支与提交规范

状态：Active v1.3

更新日期：2026-07-31

本项目采用轻量版 Conventional Commits。目标是让提交历史便于检索、回溯和学习，不为了格式引入额外运行时或复杂工具。

## 1. 基本格式

```text
<type>(<scope>): <中文简述>
```

`scope` 可省略。发生破坏性变更时，在 `type` 或 `scope` 后增加 `!`：

```text
<type>(<scope>)!: <中文简述>
```

提交信息的语言规则：

- `type` 和 `scope` 使用小写英文；
- 简述、正文和 `BREAKING CHANGE` 说明使用中文；
- 技术名词、协议名、代码标识符和官方产品名保留原文。

## 2. 类型

| 类型 | 用途 |
|---|---|
| `feat` | 新增可观察的产品或工程能力 |
| `fix` | 修复缺陷、错误行为或可靠性问题 |
| `refactor` | 不改变外部行为的代码重构 |
| `perf` | 性能、延迟、内存或资源使用优化 |
| `docs` | 仅修改文档 |
| `test` | 新增或调整测试，不改变生产行为 |
| `style` | 仅格式、空白或其他无语义修改 |
| `build` | 依赖、编译、容器镜像或构建工具 |
| `ci` | GitHub Actions、质量门禁和 CI/CD |
| `chore` | 无法归入其他类型的仓库维护，谨慎使用 |
| `revert` | 撤销已有提交 |

安全修复使用 `fix(security)`，依赖变更优先使用 `build(deps)`，不要为少见场景随意增加新类型。

## 3. 分支命名

开发分支使用与提交类型相同的前缀：

```text
<type>/<work-item>-<english-slug>
```

- `type` 使用第 2 节定义的类型，表示该分支最终 PR 的主要结果；
- 计划任务的 `work-item` 使用去掉连字符的小写稳定编号，例如 `T-006` 写作 `t006`；
- 没有稳定任务编号时，可以使用 GitHub Issue 编号，例如 `issue12`；无需关联任务的窄幅维护分支可以省略 `work-item`；
- `english-slug` 使用小写英文和连字符，简短说明分支目标；
- 不使用 `codex`、人员姓名、`wip`、`temp` 等无法表达变更目的的前缀；
- 分支可以包含不同类型的逻辑提交，前缀只需与最终 PR 和 Squash Merge 的主要类型一致。

示例：

```text
feat/t006-postgres-foundation
fix/issue12-websocket-reconnect
docs/git-branch-convention
```

不推荐：

```text
codex/t006-postgres-foundation
luigi/update
wip/database
```

`style` 分支只用于格式、空白等无语义变化，不用于 UI 样式或产品外观能力；后者应根据实际结果使用 `feat` 或 `fix`。

## 4. Scope

Scope 表示主要受影响的业务模块或工程边界。只有在能够增加识别度时才使用；跨多个模块时可以省略。

推荐沿用仓库已有名称，例如：

- `app`
- `config`
- `domain`
- `source`
- `hyperliquid`
- `storage`
- `db`
- `jobs`
- `discord`
- `ci`
- `docs`
- `deps`

Scope 使用小写英文，不使用 Issue 编号、人员姓名、临时分支名或含义重叠的缩写。

## 5. 简述与正文

简述必须：

- 使用中文说明产生的结果；
- 精确、具体，避免“更新”“修改”“优化一下”“修复问题”等模糊表达；
- 聚焦一个逻辑变化；
- 不以句号结尾；
- 不包含无关文件或顺手重构。

当原因、取舍或用户影响不明显时，在空行后添加中文正文，重点解释为什么修改，而不是逐行复述代码。

```text
fix(app): 避免关闭超时时遗留监听器

关闭超时后主动释放监听器，防止重启时端口仍被占用。
```

## 6. Issue 与破坏性变更

普通提交需要关联任务时，在 footer 使用：

```text
Refs #4
```

Issue 的自动关闭默认写在 PR 正文中，例如 `Closes #4`，避免中间提交过早绑定任务关闭语义。

破坏性变更必须同时：

1. 在标题中使用 `!`；
2. 在 footer 中使用 `BREAKING CHANGE:`，用中文说明影响和迁移方式。

```text
feat(config)!: 重命名数据库连接配置

BREAKING CHANGE: DATABASE_DSN 已更名为 DATABASE_URL，部署环境需要同步更新。
```

## 7. 提交边界与 TDD

- 每个提交只包含一个逻辑变化。
- 提交前检查 `git diff` 和待暂存文件，排除密钥、生成垃圾和无关改动。
- 推送到远端的提交必须通过适用的开发门禁。
- TDD red 阶段的故意失败测试用于本地证明需求，不作为失败提交保留在 PR 中。
- 不为了美化历史擅自强推已经共享的分支；确需重写时先明确协调。
- 不使用 `WIP`、`temp`、`misc`、`update` 或 `fix bug` 等不可追溯信息。

## 8. PR 与 Squash Merge

本仓库默认使用 squash merge，因此主分支最终提交通常来自 PR 标题。为保证 `main` 历史一致：

- PR 标题必须遵循与提交标题相同的格式；
- PR 可以包含多个符合规范的逻辑提交；
- 合并前检查最终 squash 标题仍符合本规范；
- PR 正文继续记录目标、原因、用户影响、验证结果、剩余风险和 `Closes #<issue>`；
- GitHub 自动附加的 PR 编号可以保留，例如 `ci: 建立自动化质量门禁 (#7)`。

## 9. 端到端交付授权

以 GitHub Issue 为唯一事实源实施任务时，一旦 Issue 验收标准满足且适用门禁通过，默认视为授权将当前任务完整交付到 `main`，无需等待用户再次要求“提交”。非 Issue 任务中，用户要求“提交”“完整交付”“一步到位”“完成后合并到 `main`”或表达同等含义时，同样适用。该授权覆盖以下标准交付链：

1. 创建或切换任务分支；
2. 运行适用质量门禁并复核最终差异；
3. 暂存、提交并推送任务范围内的变更；
4. 创建 PR，补全 Issue 关联、验证结果和剩余风险；
5. CI 通过且没有 review 阻塞后，将 PR 转为 Ready for Review；
6. 使用符合本规范的最终标题执行 Squash Merge 到 `main`；
7. 确认 PR 已合并、关联 Issue 已关闭，并以 fast-forward-only 方式同步本地 `main`。

获得上述授权后，不在提交、推送、创建 PR、Ready、Squash Merge 和同步 `main` 之间重复询问。过程中应提供简短状态更新，但状态更新不是新的审批请求。

执行 Squash Merge 前必须同时满足：

- 使用 `$repo-task-state` Skill 的 `merge` 模式核对当前 PR、PR head SHA、CI 和工作区状态；
- 任务范围和目标分支明确，且实施过程中没有发生实质性扩展；
- 工作区没有被误纳入的用户修改、密钥或无关文件；
- 所有适用本地门禁和必需 CI 均通过；
- PR head SHA 未变化，合并状态为 clean/mergeable；
- 没有未解决的 review 意见、requested changes 或仓库要求的待审批项；
- PR 标题、正文、Issue 关闭语义和最终 Squash 标题符合本规范。

即使已有端到端授权，遇到以下情况仍必须暂停并说明阻塞，不得自行扩大授权：

- 门禁、CI、迁移、构建或官方 smoke 失败；
- 出现合并冲突、head SHA 意外变化或无法确认的并发修改；
- review 要求变更，或修复会明显扩大原任务范围；
- 需要强推、改写共享历史、删除分支、绕过保护规则或降低测试标准；
- 平台、权限系统或外部工具要求针对当前高影响操作再次明确确认。

如果用户明确要求“仅本地提交”“不要推送”“只创建 PR”“暂不合并”或其他更窄边界，以该边界为准。如果用户只要求设计、实现或评审，尚未要求提交或交付，则不自动推送或合并；此时应在进入发布阶段前一次性说明将执行的外部操作并请求整包授权，避免逐步骤重复确认。

## 10. 项目示例

```text
feat(config): 增加数据库连接配置校验
fix(app): 避免关闭超时时遗留监听器
test(hyperliquid): 增加行情数组错位回归测试
refactor(source): 分离文档发现与原文快照
perf(hyperliquid): 限制行情状态复制次数
ci: 建立自动化质量门禁
docs(tasks): 将 T-006 标记为当前任务
build(deps): 升级 pgx 依赖
```

不推荐：

```text
update
fix bug
chore: 修改代码
feat: 添加一些功能
WIP
```

## 11. 自动化边界

当前先通过文档、代码评审和 squash 合并检查执行本规范，不引入 commitlint、Husky 或 Node.js 工具链。

如果提交格式连续出现三次可避免的问题，再在现有仓库脚本和 CI 中增加轻量校验，并确保本地与 CI 复用同一规则。

## 12. Worktree 并行交付

用户明确触发 `$worktree-delivery` 时，任务使用独立 Git worktree 完成完整交付。新 worktree 和新分支必须从执行时已核验的 `origin/main` 创建，不得从当前存在用户修改的 worktree 复制或派生。

标准生命周期：

```text
preflight → origin/main → worktree/branch → implement/test → commit
→ push → PR → CI/review → squash merge main → verify → remove worktree
```

约束：

- 当前 worktree 的已有修改必须保留，不能被新任务暂存或提交。
- 分支、路径、Issue、PR head SHA 和目标分支必须在每个高影响阶段核验。
- commit、push、PR、merge 和清理可以由一次显式 Skill 触发授权，但 CI pending、失败、review 要求、冲突、权限不足、head SHA 变化或门禁失败时必须暂停。
- 只能在 PR 已确认合并到 `main` 且 worktree 工作区干净后删除本次 Skill 创建的 worktree；不得使用强制删除掩盖未提交修改。
- 确定性的 Git/GitHub 操作优先通过 `.agents/skills/worktree-delivery/scripts/worktree_delivery.py` 执行，脚本失败时保留 worktree 并报告可恢复状态。
