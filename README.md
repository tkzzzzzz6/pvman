# pvman

<table>
	<tr>
		<td width="180" align="center">
			<img src="https://tk-pichost-1325224430.cos.ap-chengdu.myqcloud.com/blog/b842f40fe85f3b70cc2f7b89a9f6e7e0.png" alt="pvman icon" width="160">
		</td>
		<td>
			<h2 align="center">
				<img src="./assets/wordmark.png" alt="pvman" width="157" height="39">
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

### Environments 

![1790604410511.png](https://tk-pichost-1325224430.cos.ap-chengdu.myqcloud.com/blog/1790604410511.png)

### Packages

![1790604480519.png](https://tk-pichost-1325224430.cos.ap-chengdu.myqcloud.com/blog/1790604480519.png)

### Search

![1790604585208.png](https://tk-pichost-1325224430.cos.ap-chengdu.myqcloud.com/blog/1790604585208.png)

## Install

### One-line install (Linux / macOS)

The script detects your operating system and CPU architecture, downloads the
prebuilt `pvman` binary from the latest GitHub release, verifies it against the
release's `checksums.txt`, and installs it to `~/.local/bin`. No Go toolchain is
needed, nothing is compiled, and nothing else on your machine is touched:

```bash
curl -fsSL --connect-timeout 15 --max-time 60 https://raw.githubusercontent.com/tkzzzzzz6/pvman/main/scripts/install.sh | sh
```

Or use `wget`:

```bash
wget --timeout=15 --tries=1 -qO- https://raw.githubusercontent.com/tkzzzzzz6/pvman/main/scripts/install.sh | sh
```

Linux and macOS on `amd64` and `arm64` are supported. If `~/.local/bin` is not
already on your `PATH` the script adds it to `~/.zshrc`, `~/.bashrc` or
`~/.profile`. Then reload your shell and run the program:

```bash
source ~/.zshrc   # or ~/.bashrc / ~/.profile
pvman
```

Two environment variables are honoured:

| Variable             | Effect                                                      |
| -------------------- | ----------------------------------------------------------- |
| `PV_MAN_VERSION`     | Install a specific tag rather than the latest, e.g. `0.7.0`  |
| `PV_MAN_INSTALL_DIR` | Install somewhere else; this also skips the `PATH` edit      |

### One-line install (Windows)

Run the following command in PowerShell. It installs the prebuilt `pvman` binary
from the latest GitHub release for the current Windows user, verifies it against
the release's `checksums.txt`, and needs no administrator privileges:

```powershell
irm https://raw.githubusercontent.com/tkzzzzzz6/pvman/main/scripts/install.ps1 | iex
```

Alternatively, download and run the batch file from Command Prompt or PowerShell:

```bat
curl.exe -fsSL https://raw.githubusercontent.com/tkzzzzzz6/pvman/main/scripts/install.bat -o install-pvman.bat
install-pvman.bat
```

The Windows installer supports `amd64` and `arm64`, installs to
`%LOCALAPPDATA%\pvman\bin`, and appends that directory to your user `PATH`.
`PV_MAN_VERSION` and `PV_MAN_INSTALL_DIR` work here too. Open a new terminal
after installation, then run `pvman`.

### Go install

If you would rather build pvman from source, install it with the Go toolchain
you already have. pvman requires Go 1.26.1 or newer:

```bash
go install github.com/tkzzzzzz6/pvman@latest
```

The binary lands in `$(go env GOPATH)/bin`. pvman notices it was installed this
way, and `pvman --update` refreshes it with `go install ...@latest`.

If you would rather not touch your default `go`, the official versioned
toolchain works as well -- but note that `go1.26.1 download` fetches a full
~60 MB SDK into `~/sdk`, and `go1.26.1 install` fails with
`not downloaded` until you have run it:

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

### Updating

```bash
pvman --update
```

This replaces the binary in place with the newest release. pvman picks the
method from where the running binary lives: a binary in your Go bin directory is
refreshed with `go install github.com/tkzzzzzz6/pvman@latest`, and anything else
is upgraded by downloading the matching release archive and checking its SHA-256
against `checksums.txt` before swapping it in. Check the running version with
`pvman --version`.

This is also why the install scripts use `~/.local/bin` (and
`%LOCALAPPDATA%\pvman\bin`) instead of a Go bin directory. Channel detection
only looks at the binary's directory, so a release binary sitting in
`GOPATH/bin` would be mistaken for a `go install` one and try to upgrade through
a toolchain that was never set up. Moving the binary into your Go bin directory
by hand has the same effect -- leave it where the installer put it.

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

Requires Go 1.26.1 or newer.

```bash
git clone https://github.com/tkzzzzzz6/pvman.git
cd pvman
go build -ldflags="-s -w" -o pvman .
./pvman
```

## Requirements

- [conda](https://docs.conda.io/) or [miniconda](https://docs.anaconda.com/miniconda/) for conda env support
- [uv](https://docs.astral.sh/uv/) for uv env support
