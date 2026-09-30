---
name: git-commit-and-tag
description: 'Generate Conventional Commit messages and cut semantic-version tags (vX.Y.Z) for the pvman repo. Use when: writing or fixing a commit message, committing staged changes, choosing the commit type or scope, summarizing what changed for a commit, bumping the version, creating an annotated tag, pushing a release tag, deciding whether a change deserves a tag, or drafting release notes. Triggers: commit, git commit, staged changes, 提交, 提交信息, 写 commit, 打 tag, 打标签, 发版, release, version bump, next version, changelog.'
argument-hint: '[commit | tag | both] [major | minor | patch] — 例如 "commit"、"tag minor"、"both patch"'
---

# Commit Message & Release Tag for pvman

为 pvman 生成本仓库风格的提交信息，并在需要发版时计算、创建、推送语义化版本 tag。

## When to Use

- 用户让你写/改一条 commit message，或把暂存内容提交。
- 用户要求「打 tag / 发版 / bump version / 出新版本 / 写 release notes」。
- 用户问「这次改动该不该打 tag」「下一个版本号是多少」。
- 你需要为 tag 注解整理自上一个 tag 以来的变更摘要。

## When NOT to Use

- 只想看历史：直接 `git log`，不必启动本流程。
- 修改 `.github/workflows/release.yml` 的发布逻辑：那是 CI 代码变更，不是发版动作。
- 项目外的仓库：本技能里的规范与安全检查只针对 pvman。

## 硬性规则（先读完再动手）

这些规则来自本仓库的实际实现，不只是风格偏好，违反会造成真实故障：

1. **tag 即发布。** `.github/workflows/release.yml` 在 `push: tags: ['v*']` 时触发，交叉编译 6 个目标并发布 GitHub Release。**推送 `v*` tag 就等于对外发版**，因此：
   - 创建/推送 tag 前必须与用户确认版本号，不要自行决定后直接推送。
   - 只推**具体那个 tag**（`git push origin v0.7.0`），禁止 `git push --tags`。
2. **绝不重写历史。** 不 `git push --force`、不删除或移动已推送的 tag、不 amend 已推送的提交。发错了就发下一个 patch 版本。
3. **tag 必须指向通过了门禁的提交。** 发版前该提交要在 CI 绿或在本地跑过 `go build ./... && go vet ./... && go test ./...`。release workflow 自己会重跑 `go test ./...`，但它构建的是 tag 指向的树。
4. **tag 落在 `main`。** 不要给未合并的特性分支打版本 tag。
5. **版本号只增不减。** 版本比较是自研在 `internal/update/version.go`（含 prerelease 规则，`0.10.0 > 0.9.0`），不能按字符串比较；语义是严格的 semver 顺序。
6. **不要提交构建产物。** `pvman`、`pvman.exe`、`dist/` 已在 `.gitignore` 中，不要用 `-f` 强行加。
7. **行尾陷阱。** `.gitattributes` 声明 `* text=auto eol=lf`，但既有工作区大部分 `.go` 是 CRLF。不要为了提交顺手跑 `gofmt -w`（它会把 CRLF 静默改写成 LF，制造混合行尾）或 `git add --renormalize .`（会把整个仓库变 LF，污染 diff）。只提交本次真正改动的文件。

## 提交类型与 scope 约定

历史里 `feat:` / `fix:` / `docs:` 与英文描述句混用，本技能统一到 Conventional Commits：

| type | 用于 |
| --- | --- |
| `feat` | 新增用户可见能力（新视图、新按键、新命令） |
| `fix` | 修 bug、修越界/解析/过期结果等问题 |
| `docs` | README、Agent.md、注释、安装说明 |
| `refactor` | 不改行为的结构调整 |
| `test` | 只动 `*_test.go` 或测试夹具 |
| `ci` | `.github/workflows/`、发布/校验链路 |
| `build` | `go.mod`、`go.sum`、构建/安装脚本 |
| `perf` | 明确的性能改动 |
| `chore` | 其余杂务（版本徽章、图片资源等） |

scope 取改动所属的模块（可省略）：

| scope | 对应区域 |
| --- | --- |
| `ui` | `internal/ui/*`（model、keys、styles） |
| `conda` | `internal/conda/*` |
| `uv` | `internal/uv/*` |
| `update` | `internal/update/*`、自更新渠道逻辑 |
| `install` | `scripts/install.*` |
| `release` | `.github/workflows/release.yml`、`test/verify-release.sh` |
| `ci` | `.github/workflows/ci.yml` |
| `agent` | `Agent.md` |
| `skills` | `.github/skills/` |

**主题行语言**：跟随用户本次请求的语言；用户没说时，向最近的历史看齐（英文用大写起首的祈使句，中文直接写中文，如 `fix:环境列表的 * 标注移到名字旁`）。冒号后不加空格也可，但与同一句内保持一致。

格式：

```
<type>(<scope>): <subject>

<body: 为什么这么改、权衡、非显然的副作用>

<footer: 关联 issue / BREAKING CHANGE: ...>
```

规则：主题 ≤ 72 字符、不加句末句号；body 用来说明「为什么」而不是复述 diff；破坏性变更用 `type!:` 或 `BREAKING CHANGE:` footer 显式标出。

## 流程 A — 生成并创建提交

1. **收集上下文**（不要凭记忆猜）：

   ```bash
   git status --short
   git diff --stat
   git diff --staged
   git branch --show-current
   git tag --list 'v[0-9]*' --sort=-v:refname | head -n 3
   ```

2. **跑门禁**，按改动范围选择：

   ```bash
   go build ./... && go vet ./... && go test ./...
   ```

   只改文档/脚本时可跳过测试，但要说明跳过了。改了 `.go` 就额外做格式检查——**不要**用 `gofmt -l .`（CRLF 会全部误报），用归一化比较：

   ```bash
   diff -q <(tr -d '\r' < f.go) <(tr -d '\r' < f.go | gofmt) >/dev/null \
     && echo 已格式化 || echo 未格式化
   ```

3. **明确暂存范围**：优先 `git add <具体文件>`；`git add -A` / `git commit -a` 会把无关改动和行尾变更一起带进来。确认 `git diff --staged --stat` 里没有构建产物、没有整文件行尾翻转。

4. **写 message**：按上面的类型/scope 表定稿；一次逻辑改动一个提交，不要把无关重构混进来。

5. **提交**：用 heredoc 传多行 message，避免 `-m` 拼串。

6. **汇报**：给出提交哈希、`type(scope): subject`、是否还建议跟一个 tag（以及为什么）。

## 流程 B — 计算、创建并推送 tag

1. **核对前提**：当前在 `main`、工作区干净、门禁通过、有值得发版的提交。

2. **看自上个 tag 以来的变更**：

   ```bash
   bash .github/skills/git-commit-and-tag/scripts/summarize_changes.sh
   ```

3. **算下一个版本号**：

   ```bash
   bash .github/skills/git-commit-and-tag/scripts/next_version.sh auto --dry-run
   ```

   `auto` 的判定：有破坏性变更时——`major > 0` 进 major、`0.x` 进 minor；否则 `feat:` 进 minor，其余进 patch。用户指定了 `major|minor|patch` 就直接传该参数覆盖。版本号输出在 stdout，诊断信息在 stderr，便于 `$(...)` 取值。

4. **与用户确认版本号**（明确告知：推送后 CI 会立刻发 Release）。

5. **创建带注解的 tag**，注解用上一步的摘要：

   ```bash
   git tag -a v0.7.0 -m "v0.7.0" -m "$(bash .github/skills/git-commit-and-tag/scripts/summarize_changes.sh)"
   ```

   tag 名前缀必须是 `v`（`v*` 才触发 release，其它名字不会发布也不会被版本逻辑识别）。

6. **推送这一个 tag**：

   ```bash
   git push origin v0.7.0
   ```

   然后提醒用户去看 release workflow 的结果，以及发布后可用 `bash test/verify-release.sh v0.7.0` 校验产物。

## 快速参考

```bash
# 最新版本 tag（按版本序，不按字典序）
git tag --list 'v[0-9]*' --sort=-v:refname | head -n 1

# 自上个 tag 以来的提交
git log --oneline "$(git describe --tags --abbrev=0)"..HEAD

# 已有 tag 清单
git --no-pager tag

# 下一个版本号（仅打印，不建 tag）
bash .github/skills/git-commit-and-tag/scripts/next_version.sh auto --dry-run
```

## 参考

- 变更摘要脚本：[./scripts/summarize_changes.sh](./scripts/summarize_changes.sh)
- 版本号计算脚本：[./scripts/next_version.sh](./scripts/next_version.sh)
- 约定与发版语义详解：[./references/conventions.md](./references/conventions.md)
