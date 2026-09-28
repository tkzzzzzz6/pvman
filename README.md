# pvman

<table>
	<tr>
		<td width="180" align="center">
			<img src="https://tk-pichost-1325224430.cos.ap-chengdu.myqcloud.com/blog/b842f40fe85f3b70cc2f7b89a9f6e7e0.png" alt="pvman icon" width="160">
		</td>
		<td>
			<h2 align="center">
				<span style="font-family: 'Hiragino Maru Gothic ProN', 'Yu Gothic', 'Comic Sans MS', cursive; font-size: 1.4em; font-style: italic; font-weight: 900; letter-spacing: 0.08em; padding: 0 10px 4px; border-bottom: 3px solid #9bdcff; text-shadow: 1px 1px 0 #d9f3ff;">
					<span style="color: #4db8ff;">pv</span><span style="color: #ff4d5a;">man</span>
				</span>
			</h2>
			A terminal UI for managing Python virtual environments, with conda and uv side by side.
		</td>
	</tr>
</table>

[![CI](https://github.com/tkzzzzzz6/pvman/actions/workflows/ci.yml/badge.svg)](https://github.com/tkzzzzzz6/pvman/actions/workflows/ci.yml)
[![Go Version](https://img.shields.io/github/go-mod/go-version/tkzzzzzz6/pvman?logo=go&style=flat)](go.mod)
[![Platform](https://img.shields.io/badge/platform-macOS%20%7C%20Linux%20%7C%20Windows-lightgrey?style=flat)](https://github.com/tkzzzzzz6/pvman)
[![License](https://img.shields.io/badge/license-MIT-green?style=flat)](LICENSE)

## Features

- View all **conda** environments with Python version, package count, and size
- Scan current directory for **uv** virtual environments (`.venv` and named envs)
- Async detail loading — size and package info loads in the background
- Browse installed packages and remove them, one at a time or in bulk
- Live dependency panel — highlight a package to see what it needs and what needs it
- Cascade-aware removal: the delete dialog lists the packages your selection would break or orphan, and lets you take them along
- Create new uv venvs with a specific Python version
- Delete conda or uv environments with confirmation
- Activate an environment in a new shell with a single keystroke
- One-click copy of the activation command
- Filter environments and packages as you type with `f`
- Vim-style (`j`/`k`) and arrow key navigation

## Demo

### Environment 

![1790529914591.png](https://tk-pichost-1325224430.cos.ap-chengdu.myqcloud.com/blog/1790529914591.png)

## Install

### One-line install (Linux / macOS)

The install script automatically detects the operating system and CPU architecture,
downloads Go 1.26.1 to a side directory (`~/.local/go1.26.1`), adds a `go1.26.1`
wrapper, and installs `pvman`. It does **not** change your default `go` command:

```bash
curl -fsSL --connect-timeout 15 --max-time 60 https://raw.githubusercontent.com/tkzzzzzz6/pvman/main/scripts/install.sh | sh
```

Or use `wget`:

```bash
wget --timeout=15 --tries=1 -qO- https://raw.githubusercontent.com/tkzzzzzz6/pvman/main/scripts/install.sh | sh
```

The script supports Linux and macOS on `amd64` and `arm64`. After installation,
reload your shell and run the program:

```bash
source ~/.bashrc  # or use ~/.zshrc for zsh
pvman
```

You can also use the downloaded Go directly without affecting your system `go`:

```bash
go1.26.1 build -ldflags="-s -w" -o pvman .
```

<!-- To install another Go version, set `PV_MAN_GO_VERSION` before running the script:

```bash
curl -fsSL --connect-timeout 15 --max-time 60 https://raw.githubusercontent.com/tkzzzzzz6/pvman/main/scripts/install.sh | PV_MAN_GO_VERSION=1.26.1 sh
``` -->

### One-line install (Windows)

Run the following command in PowerShell. It downloads Go 1.26.1 to a side
directory, adds a `go1.26.1` wrapper, and installs `pvman` for the current
Windows user, without requiring administrator privileges. Your default `go`
command is left untouched:

```powershell
irm https://raw.githubusercontent.com/tkzzzzzz6/pvman/main/scripts/install.ps1 | iex
```

Alternatively, download and run the batch file from Command Prompt or PowerShell:

```bat
curl.exe -fsSL https://raw.githubusercontent.com/tkzzzzzz6/pvman/main/scripts/install.bat -o install-pvman.bat
install-pvman.bat
```

The Windows installer supports `amd64` and `arm64`. Open a new terminal after
installation, then run `pvman` or `go1.26.1`.

### Go install

If you already have a working `go` installation, you can install a versioned
`go1.26.1` command without touching your default `go`. The first time you use
`go1.26.1`, download the full toolchain:

```bash
go install golang.org/dl/go1.26.1@latest
go1.26.1 download
go1.26.1 install github.com/tkzzzzzz6/pvman@latest
```

## Usage

Run `pvman` from any project directory:

```bash
pvman
```

It will show all your conda environments and scan the current directory for uv venvs.

Press `enter` on a selected environment to open a new shell with that environment
activated. Type `exit` in the shell to return to `pvman`.

Press `p` on a selected environment to browse its installed packages. In the
package view, use `space` to tick the packages you want to remove, `a` to toggle
select-all, and `d` to delete everything that is ticked (with a confirmation).
Press `esc` to go back to the environment list.

## Key Bindings

### Environment list


| Key        | Action                                            |
| ---------- | ------------------------------------------------- |
| `j` / `↓` | Move down                                         |
| `k` / `↑` | Move up                                           |
| `enter`    | Activate selected environment (opens a new shell) |
| `p`        | Browse and remove packages                        |
| `n`        | Create new uv environment                         |
| `d`        | Delete selected environment                       |
| `f`        | Filter the list by name                           |
| `r`        | Refresh list                                      |
| `q`        | Quit                                              |
| `esc`      | Cancel / close dialog                             |

### Package list


| Key               | Action                                             |
| ----------------- | -------------------------------------------------- |
| `j` / `↓`        | Move down                                          |
| `k` / `↑`        | Move up                                            |
| `space`           | Tick / untick the highlighted package              |
| `a`               | Select all (press again to clear)                  |
| `f`               | Filter the list by name                            |
| `d`               | Delete all ticked packages (asks for confirmation) |
| `esc` / `q` / `p` | Back to the environment list                       |

### Filtering

`f` opens a filter box in place of the status line. Typing narrows the list to
the rows containing what you type, ignoring case — `ng` finds `libgcc-ng`, and
`REQ` finds `requests` and `requests-toolbelt` alike. The box takes the
keyboard while it is open, so nothing you type can tick, delete or quit.


| Key         | Action                                                       |
| ----------- | ------------------------------------------------------------ |
| `↑` / `↓` | Move the cursor through the matches, without closing the box |
| `enter`     | Close the box and keep the filter on the list                |
| `esc`       | Close the box and clear the filter.                          |

With a filter applied, `a` selects the packages **on screen** — a tick you made
before typing is neither extended to the hidden rows nor dropped, and the title
reports the subset as `3/631 packages`.

A filter belongs to the list it was typed on: entering an environment's package
list starts unfiltered, and leaving it clears the filter again.

The panel on the right follows the cursor and shows the highlighted package's
dependencies in both directions: **needs** (what it requires) and **needed by**
(what requires it). When the list is too narrow for two columns it falls back to
a single centred panel.

`d` opens a confirmation that names the packages the selection is entangled
with, split into two groups:

- **broken** — packages that would be left with a requirement nothing
  satisfies. These are found by walking the reverse dependency edges
  transitively.
- **orphaned** — the selection's own dependencies that nothing else in the
  environment needs, so they would be left unused. This is transitive too:
  removing an orphan can orphan its own dependencies. A package the selection
  does not reach is never listed, so something you installed deliberately stays
  put.


| Key   | Action                                                        |
| ----- | ------------------------------------------------------------- |
| `y`   | Delete the ticked packages**and** everything listed           |
| `n`   | Delete only the ticked packages, leaving the rest as it falls |
| `esc` | Cancel, deleting nothing                                      |

Note that **pip and uv do not cascade** — `n` really does leave dependents
broken. conda's solver, by contrast, removes far more than it is asked to: a
single package pulled in by a metapackage can take hundreds with it. The status
line reports the number conda actually removed, read back from its own
transaction summary, rather than the number you asked for.

`ctrl+c` quits from any view.

## Build from source

```bash
go install golang.org/dl/go1.26.1@latest
go1.26.1 download
git clone https://github.com/tkzzzzzz6/pvman.git
cd pvman
go1.26.1 build -ldflags="-s -w" -o pvman .
./pvman
```

## Requirements

- [conda](https://docs.conda.io/) or [miniconda](https://docs.anaconda.com/miniconda/) for conda env support
- [uv](https://docs.astral.sh/uv/) for uv env support
