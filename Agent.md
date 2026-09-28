# pvman — agent context

面向接手此仓库的 coding agents。本文是代码导航与实现约束；修改前以当前源码、`go.mod` 和测试为准，不把 README 的功能描述当成已验证行为。按请求使用文件名 `Agent.md`；不要假定 agent 工具会像 `AGENTS.md` 一样自动加载它。

## 项目与边界

- Go 单二进制 TUI，用于查看、激活、删除 Python 环境，浏览/批量删除包，分析包依赖；仅支持创建 uv 环境。
- 模块：`github.com/tkzzzzzz6/pvman`；`go.mod` 声明 Go `1.26.1`。
- UI：Bubble Tea `v1.3.10`、Bubbles `v1.0.0`、Lip Gloss `v1.1.0`。
- 目标平台：Windows、Linux、macOS；运行时调用外部 `conda`、`uv` 及目标环境的 Python。不是 Python 项目，无 Web 服务、数据库或独立配置存储。
- 启动目录有业务意义：`ui.New()` 保存 cwd，uv 扫描与创建均以此为基准。扫描仅检查 cwd 的直接子目录是否存在 `pyvenv.cfg`，不递归，也不验证该 venv 是否由 uv 创建。

## 文件导航

| 路径 | 职责 / 修改入口 |
| --- | --- |
| `main.go` | `ui.New()` → `tea.NewProgram(..., tea.WithAltScreen())`；启动错误写 stderr 并退出 |
| `internal/ui/model.go` | 核心单文件：Model、状态机、异步命令/消息、键盘路由、依赖关系分析、过滤、布局、shell 激活 |
| `internal/ui/keys.go` | 按键绑定；实际路由在 `handleKey` / `handleFilterKey`，提示还涉及 `hints` 等渲染函数 |
| `internal/ui/styles.go` | Lip Gloss 样式、颜色、`formatSize` |
| `internal/conda/conda.go` | 环境发现、详情、包列表、conda-meta 依赖解析、conda/pip 删除、激活命令 |
| `internal/uv/uv.go` | venv 扫描/创建/删除、uv pip 包操作、Python 元数据依赖解析、激活命令 |
| `internal/{ui,conda,uv}/*_test.go` | 同包测试，可直接测试未导出函数；主要覆盖纯逻辑、状态转换、布局和临时文件元数据 |
| `scripts/install.sh` / `install.ps1` / `install.bat` | 用户安装流程；bat 下载并执行远端 PowerShell 脚本 |
| `test/verify-release.sh` | 校验已发布的 Release：校验和、包内文件、以及二进制头部是否真属于文件名声称的平台与架构 |
| `.github/workflows/ci.yml` | 三平台矩阵的 build / vet / test / gofmt 门禁 |
| `.github/workflows/release.yml` | `v*` tag 触发；交叉编译六目标并发布到 GitHub Release |
| `.gitattributes` | 行尾策略；改动 Go 文件前先读「开发与验证」里的行尾说明 |
| `README.md` | 用户文档；行为变更时同步，但先核实实现 |

## UI 数据流与必须保留的语义

- Bubble Tea 值类型 Model：`Init` 启动 spinner 与环境加载；耗时工作放在 `tea.Cmd`，结果通过消息进入 `Update`。避免在按键处理或 `View` 内直接运行子进程/扫描目录。
- 环境详情按当前选中项懒加载，以 `Loaded` 缓存。包列表和依赖图一起加载；依赖图构建失败仍允许显示包列表。
- `listItem.idx` 指向后端环境切片；环境 `cursor` 指向 `envList()` 的可见行，必须跳过 header。
- `pkgCursor` 指向可见行；`filteredPkgs` 保存源索引；`pkgSelected` 的 key 是 `packages` 的源索引。通过 `pkgAt()` 转换，不能直接用可见游标操作源切片。
- 过滤切片为 nil 表示未过滤；非 nil 空切片表示零匹配。取消勾选要 delete map entry，不能保留 false，因为 `len(pkgSelected)` 用作选中数。
- `f` 输入为忽略大小写的子串匹配；输入框持有键盘焦点时吞掉普通快捷键。Enter 保留过滤并关闭输入框，Esc 清除过滤；上下方向键仍可导航。
- 过滤下 `a` 只切换可见包，隐藏包的勾选保留；进入/离开包视图清理过滤，离开时还清理依赖图和选择状态。
- 异步结果可能在列表刷新、缩短或切换视图后返回。保留索引校验、包消息的视图/环境匹配检查，以及环境刷新取消环境删除确认的行为。
- 当前异步环境身份主要使用 type + index，并非稳定 ID/请求版本；修改刷新逻辑时注意同索引对应另一环境的可能性，不要认为边界检查已解决全部过期结果问题。
- 布局必须适应窄/矮终端。包视图有双列与紧凑布局，删除弹窗也有紧凑模式；使用现有尺寸计算与 Lip Gloss 测量方式，状态栏保持一行，提示按完整键值对裁减。

## 删除与依赖图

- `deps[p]` = p 依赖谁；`dependents[p]` = 谁依赖 p。图只保留已安装包之间的边。
- `selectionRelation()` 计算传递的 Breaks（会失去依赖的包）与 Orphans（删除集合可达、且不再被其他包需要的依赖）。无关的独立包不应被加入；循环必须终止。
- 打开包删除确认时保存 `rel`；`deleteSet()` 统一产生提交集合。
- **包删除弹窗**：`y` 删除选中及关联包；`n` 仍执行删除，但只提交选中包；Esc 才取消。**环境删除弹窗**的 `n` 则取消。不要统一成同一语义。
- conda 按 `-p env.Path` 列包/删除环境/删除包。来自 `pypi` channel 的包改由目标 Python 的 `-m pip uninstall -y` 删除；其余交给 conda solver。
- conda solver 可能额外删除包，返回数量解析 `will be REMOVED` 事务输出；解析不到时退回请求数。uv/pip 不自动级联，返回请求数量。
- uv 删除环境直接调用 `os.RemoveAll(env.Path)`。后端没有确认 UI；确认由 UI 层负责。验证删除行为使用临时/可丢弃环境，勿对真实用户环境试删。

## 后端差异与已知限制

- conda base 优先用 `conda info --json` 的 `root_prefix` 识别，失败才按发行版目录后缀回退；Windows 路径比较忽略大小写。
- Python 路径：Windows conda 为 `<env>/python.exe`，Windows venv 为 `<env>/Scripts/python.exe`；Unix 均为 `<env>/bin/python`。
- conda 依赖图仅来自 `conda-meta/*.json`，不能视为完整的 pip 依赖图；损坏记录会跳过。
- uv 列包先尝试 JSON 再回退文本；依赖图由目标 Python 的 `importlib.metadata` 输出 JSON。名称匹配用 `normalizeName` 处理大小写和 `-_.`。
- `reqName` 是简化解析：跳过含 extra 条件的 requirement，其他环境 marker 被截去而非求值；不是完整依赖求解器。
- `activateCmd()` 用 `tea.ExecProcess` 暂停 TUI 并接管终端，退出子 shell 后回到 TUI；不是修改父 shell 环境。Windows 根据 `PSModulePath` 选择 powershell/cmd，Unix 使用 `$SHELL`（缺省 bash），conda 会先运行 shell hook。
- 当前 uv Windows 激活字符串是 `activate.bat`，包括走 PowerShell 分支时；conda 激活用环境名。更改时需实测 shell 与含空格路径，不要假定所有 shell 都兼容。
- README 提到一键复制激活命令，但当前按键路由没有对应实现；README 的“任意视图 Ctrl+C 退出”也不是所有状态分支都实现。不要据此向下游宣称这些行为已支持。

## 开发与验证

从仓库根目录运行，先用 `go version` 核实工具链；若使用版本化 wrapper，将下列 `go` 换为 `go1.26.1`。

```text
go run .
go build .
go test ./...
go vet ./...
go test ./internal/ui -run TestPkgFilter
```

- `go run .` 需要交互终端，会读取启动目录和本机环境；纯逻辑验证优先使用测试。
- **行尾陷阱，先读这条再动 Go 文件。** `.gitattributes` 规定 `* text=auto eol=lf`，git 里存 LF；但 `core.autocrlf=true` 且工作区是既有检出，所以**磁盘上多数 `.go` 文件目前仍是 CRLF**（9 个里 6 个）；已是 LF 的是少数，且纯属历史遗留、不是约定——别照着它们推断哪个文件"应该"是什么行尾，看 `.gitattributes`。后果：`gofmt -l .` 会把每个 CRLF 文件都判为未格式化，而 **`gofmt -w` 会把 CRLF 静默改写成 LF**——于是你改过的文件变 LF、没改的仍 CRLF，制造混合行尾。
  - 判断格式而非行尾：把去 CR 的内容分别与 `gofmt` 的输出比较，两者一致即已格式化。

    ```bash
    diff -q <(tr -d '\r' < f.go) <(tr -d '\r' < f.go | gofmt) >/dev/null \
      && echo 已格式化 || echo 未格式化
    ```

    不要用 `tr -d '\r' < f.go | gofmt -l /dev/stdin`：Windows 上 gofmt 取不到该路径，会以 `GetFileAttributesEx /proc/self/fd/0` 失败并返回 2。
  - 提交前不要依赖 `gofmt -l .` 的空输出；CI 的 gofmt 步骤是对归一化副本判定的，本地不做归一化会得到不同结论。
  - 一次性把工作区统一成 LF：`git add --renormalize . && git checkout -- .`。
- 状态机/过滤/依赖分析的变更参考现有测试补充回归；涉及 subprocess 或 shell 的变更另做对应平台验证。
- 现有单元测试无需实际 conda/uv 环境；它们不等于真实 CLI 集成验证。尤其 `TestDependenciesOverScriptOutput` 用内存 fixture 检查解析，不执行 Python。
- 安装脚本默认下载独立 Go 1.26.1，可用 `PV_MAN_GO_VERSION` 覆盖；创建版本化 wrapper，安装 `github.com/tkzzzzzz6/pvman@latest` 并更新用户 PATH/shell 配置。不要把运行安装脚本当成本地源码验证，它安装的是远端版本。
- 没有 Makefile，构建与发布全在 `.github/workflows/` 里；构建产物 `pvman`、`pvman.exe`、`dist/` 已被 `.gitignore` 忽略。
- 发布链路的自检：`bash test/verify-release.sh v0.6.0` 拉回已发布的 Release 逐项校验，`--dir <目录>` 则校验手上已有的产物。注意 `--dir` 下若目录里没有 `checksums.txt`，脚本会跳过校验和一项并明确说明，只做包内文件与二进制头部检查。
