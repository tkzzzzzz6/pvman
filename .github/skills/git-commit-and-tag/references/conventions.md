# pvman 提交与发版约定

本文件解释 `.github/skills/git-commit-and-tag/` 背后的规范来源，以及为什么某些步骤是硬性要求。

## 提交信息

### 主题行

```
<type>(<scope>): <subject>
```

- `type` 取 `feat` / `fix` / `docs` / `refactor` / `test` / `ci` / `build` / `perf` / `chore`。
- `scope` 见下表，省略时写成 `<type>: <subject>`。
- 主题 ≤ 72 字符，不加句末句号，描述「改了什么」而不是「改了哪个文件」。
- 语言跟随用户请求；混用中英在历史里存在（`feat:展示包依赖关系并在删除时提示级联影响` 与 `Add CI and release workflows` 并存），因此**同一提交内保持一致**即可，不强制统一语言。

### scope 与代码区域的对应

| scope | 路径 |
| --- | --- |
| `ui` | `internal/ui/`（`model.go` 是核心单文件：状态机、异步消息、按键路由、依赖分析、布局） |
| `conda` | `internal/conda/` |
| `uv` | `internal/uv/` |
| `update` | `internal/update/`（自更新渠道识别、`version.go` 版本比较、平台替换） |
| `install` | `scripts/install.sh` / `install.ps1` / `install.bat` |
| `release` | `.github/workflows/release.yml`、`test/verify-release.sh` |
| `ci` | `.github/workflows/ci.yml` |
| `agent` | `Agent.md` |
| `skills` | `.github/skills/` |
| `docs` | `README.md` |

### body 与 footer

- body 写「为什么」：权衡、被排除的方案、非显然的副作用（例如「包删除弹窗的 `n` 语义与环境删除弹窗不同，不能统一」这类约束变更）。
- 破坏性变更必须显式：主题用 `feat!:` / `fix!:`，或在 footer 写 `BREAKING CHANGE: <说明>`。
- 一次提交一个逻辑改动。仓库历史里的提交普遍较聚焦，不要在一个提交里混入无关格式化。

### 与仓库历史的关系

`v0.1.0`–`v0.5.0` 期间的提交有相当一部分没有 type 前缀（如 `Add \`pvman --update\` with install-channel awareness`、`updates`、`d`）。本技能新提交一律带 type 前缀，因为版本工具链（`next_version.sh` 的自动判定、变更分组）依赖它；旧提交不需要回溯修改。

## tag 与发版语义

### `v*` tag 就是发布动作

`.github/workflows/release.yml`：

```yaml
on:
  push:
    tags: ['v*']
  workflow_dispatch:
    inputs:
      tag: ...
```

推送 `v*` tag 会立即：在 ubuntu 上跑 `go test ./...` → 交叉编译 `linux/darwin/windows × amd64/arm64` → 打包并发布 GitHub Release。`v*` 匹配 `v0.5.0`，不匹配 `wip` 这类杂名，所以 tag 名必须带 `v` 前缀。

因此：**建 tag 前确认版本号，推送前再次确认。** 出错只能靠下一个 patch 版本修正，不能删 tag 重发（会与已发布资产和用户已下载的二进制冲突）。

### 版本号如何被消费

- release 构建通过 `-ldflags -X main.version=${version}` 把 tag 版本注入二进制；`pvman --version` 与 `pvman --update` 都依赖它。
- `go install` 渠道的版本来自 `debug.ReadBuildInfo()`；VCS 检出的伪版本（如 `v0.6.2-0.2026...+dirty`）一律视为 `dev`，拒绝自更新。所以 tag 格式必须干净：`v<major>.<minor>.<patch>`。
- 版本比较实现在 `internal/update/version.go`，含 prerelease 规则，`0.10.0 > 0.9.0`。**不要**用字符串比较推断版本顺序，也不要依赖 `git tag` 的默认字典序——用 `--sort=-v:refname`。

### 自动 bump 的判定规则

`scripts/next_version.sh auto`：

| 条件 | 结果 |
| --- | --- |
| 存在 `BREAKING CHANGE:` 或 `type!:`，且标量 `major > 0` | major |
| 存在 `BREAKING CHANGE:` 或 `type!:`，且 `major == 0` | minor（0.x 阶段 semver 允许破坏性变更走 minor） |
| 否则存在 `feat:` | minor |
| 其余 | patch |

脚本还会自动避免与已存在的 tag 冲突（冲突则继续递增 patch）。

### 发版前检查清单

1. 在 `main`，工作区干净。
2. `go build ./... && go vet ./... && go test ./...` 通过（release workflow 会重跑 test，但本地要先把门禁过掉再打 tag）。
3. 用 `summarize_changes.sh` 看自上个 tag 以来的变更，确认没有混入未完成的改动。
4. 版本号与用户确认。
5. `git tag -a` 建注解 tag，`git push origin <tag>` 只推这一个。
6. 发布后用 `bash test/verify-release.sh <tag>` 拉回产物校验：校验和、包内文件、二进制头部是否真属于文件名声称的 platform/arch。

## 两个容易踩的坑

### 行尾

`.gitattributes` 是 `* text=auto eol=lf`，但既有检出里多数 `.go` 在磁盘上是 CRLF。后果：

- `gofmt -l .` 会把每个 CRLF 文件判为未格式化，输出无参考价值。
- `gofmt -w` 会把 CRLF 静默改成 LF，于是你只改了逻辑却制造出整文件 diff。
- `git add --renormalize .` 会把整个仓库一次性转成 LF，同样污染 diff。

判断「真格式问题」用归一化比较：

```bash
diff -q <(tr -d '\r' < f.go) <(tr -d '\r' < f.go | gofmt) >/dev/null \
  && echo 已格式化 || echo 未格式化
```

不要用 `tr -d '\r' < f.go | gofmt -l /dev/stdin`：Windows 上 gofmt 取不到该路径会失败。

### 安装脚本与 Release 是两条互不相交的路径

`scripts/install.sh` 用 `go install` 从源码构建，**不下载** Release 里的二进制；Release 附件只服务直接下载的人。所以改了发布产物不会影响走安装脚本的用户，反之亦然。写 commit / release notes 时不要声称两者会一起生效。

## 技能自带脚本

| 脚本 | 用途 |
| --- | --- |
| [`./scripts/next_version.sh`](./scripts/next_version.sh) | 算出下一个 `vX.Y.Z`，`--dry-run` 附带区间、提交数、bump 类型 |
| [`./scripts/summarize_changes.sh`](./scripts/summarize_changes.sh) | 自上 tag 以来的提交按 Conventional 类型分组，输出 markdown，用于 tag 注解或 release notes |

两个脚本都只读 git 状态，不写仓库、不建 tag、不推送。
