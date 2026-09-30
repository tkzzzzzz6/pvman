#!/bin/sh
# pvman installer for Linux and macOS.
#
# Downloads the prebuilt release archive rather than compiling from source, so
# no Go toolchain is needed and nothing else on the machine is touched.
#
# The binary goes to ~/.local/bin -- deliberately *outside* the Go bin
# directory. internal/update tells its two install channels apart by looking at
# the directory the running binary lives in, and a binary here must take the
# release-binary path so `pvman --update` never reaches for a Go toolchain that
# this installer did not install.
#
# Environment variables:
#   PV_MAN_VERSION      install this tag instead of the latest one (e.g. 0.7.0)
#   PV_MAN_INSTALL_DIR  install somewhere else; also skips the PATH edit
set -eu

REPO="tkzzzzzz6/pvman"
RELEASES="https://github.com/${REPO}/releases"
INSTALL_DIR="${PV_MAN_INSTALL_DIR:-${HOME}/.local/bin}"
BIN="${INSTALL_DIR}/pvman"
LEGACY="${HOME}/go/bin/pvman"
TOTAL=3
RC=""

if [ -t 2 ]; then TTY=1; else TTY=0; fi

step() { printf '\n[%s/%s] %s\n' "$1" "$TOTAL" "$2" >&2; }
info() { printf '      %s\n' "$*" >&2; }
warn() { printf 'pvman: warning: %s\n' "$*" >&2; }
die()  { printf 'pvman: %s\n' "$*" >&2; exit 1; }

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT

case "$(uname -s)" in
    Linux)  GOOS=linux ;;
    Darwin) GOOS=darwin ;;
    *) die "unsupported operating system: $(uname -s); use WSL or Git Bash on Windows" ;;
esac

case "$(uname -m)" in
    x86_64|amd64)  GOARCH=amd64 ;;
    aarch64|arm64) GOARCH=arm64 ;;
    *) die "unsupported architecture: $(uname -m)" ;;
esac

if command -v curl >/dev/null 2>&1; then
    FETCH=curl
elif command -v wget >/dev/null 2>&1; then
    FETCH=wget
else
    die "need curl or wget to download the release"
fi

# macOS ships shasum but not sha256sum; most Linux distributions are the other
# way round, so one of the two is always present.
if command -v sha256sum >/dev/null 2>&1; then
    SHA256="sha256sum"
elif command -v shasum >/dev/null 2>&1; then
    SHA256="shasum -a 256"
else
    die "need sha256sum or shasum to verify the download"
fi

# resolve_tag asks GitHub which release is newest by reading the redirect that
# /releases/latest answers with. This deliberately avoids the REST API: that
# endpoint is rate-limited to 60 requests an hour for anonymous callers, and an
# installer that fails on a rate limit fails for no good reason.
resolve_tag() {
    loc=""
    if [ "$FETCH" = curl ]; then
        loc="$(curl -sI -o /dev/null -w '%{redirect_url}' \
            --connect-timeout 15 --max-time 30 "${RELEASES}/latest" || true)"
    else
        loc="$(wget --spider -S --timeout=15 --tries=1 "${RELEASES}/latest" 2>&1 \
            | awk 'tolower($1) == "location:" { sub(/\r$/, "", $2); l = $2 } END { print l }' \
            || true)"
    fi
    case "$loc" in
        */tag/*) printf '%s' "${loc##*/tag/}" ;;
        *) return 1 ;;
    esac
}

download() { # <url> <destination>
    if [ "$FETCH" = curl ]; then
        if [ "$TTY" = 1 ]; then
            curl -fL --progress-bar --connect-timeout 15 --max-time 600 \
                --retry 3 --retry-delay 2 -o "$2" "$1"
        else
            curl -fsSL --connect-timeout 15 --max-time 600 \
                --retry 3 --retry-delay 2 -o "$2" "$1"
        fi
    elif [ "$TTY" = 1 ]; then
        wget --timeout=15 --tries=3 -O "$2" "$1"
    else
        wget -q --timeout=15 --tries=3 -O "$2" "$1"
    fi
}

# fetch_quiet grabs a small metadata file. Drawing a progress bar for a 600-byte
# checksums.txt produces more output than the file has bytes.
fetch_quiet() { # <url> <destination>
    if [ "$FETCH" = curl ]; then
        curl -fsSL --connect-timeout 15 --max-time 60 --retry 3 --retry-delay 2 -o "$2" "$1"
    else
        wget -q --timeout=15 --tries=3 -O "$2" "$1"
    fi
}

# installed_version reports the version of an existing pvman without ever
# running it. Asking a binary for its own version is not safe here: --version
# only exists from v0.6.2 on, and older builds ignore their arguments and
# launch the full-screen TUI, which would seize the terminal or hang an
# unattended install. `go version -m` only reads the file.
installed_version() {
    [ -f "$1" ] || return 1
    command -v go >/dev/null 2>&1 || return 1
    # The mod line carries the full module path ("github.com/owner/repo"), not
    # the bare owner/repo that $REPO holds, so compare on the suffix.
    v="$(go version -m "$1" 2>/dev/null \
        | awk -v m="$REPO" '$1 == "mod" && ($2 == m || $2 ~ ("/" m "$")) { print $3; exit }')"
    case "$v" in
        ""|"(devel)")
            # Fallback for builds that were not made with -trimpath. Official
            # releases are built with it, and the toolchain then omits the
            # -ldflags build setting entirely, so main.version is simply not
            # recorded in them -- this branch is for local builds only.
            v="$(go version -m "$1" 2>/dev/null \
                | sed -n 's/.*main\.version=\([0-9][^" ]*\).*/\1/p' | head -n 1)"
            ;;
    esac
    case "$v" in
        ""|"(devel)") return 1 ;;
        *) printf '%s' "${v#v}" ;;
    esac
}

PREV="$(installed_version "$BIN" || true)"

step 1 "Resolving the version to install"
if [ -n "${PV_MAN_VERSION:-}" ]; then
    TAG="v${PV_MAN_VERSION#v}"
    info "pinned by PV_MAN_VERSION: ${TAG}"
else
    TAG="$(resolve_tag)" \
        || die "could not determine the latest release; pin one with PV_MAN_VERSION=0.7.0"
    info "latest release: ${TAG}"
fi
VERSION="${TAG#v}"
ARCHIVE="pvman_${VERSION}_${GOOS}_${GOARCH}.tar.gz"

step 2 "Downloading pvman ${VERSION} for ${GOOS}/${GOARCH}"
download "${RELEASES}/download/${TAG}/${ARCHIVE}" "${TMP_DIR}/${ARCHIVE}"
fetch_quiet "${RELEASES}/download/${TAG}/checksums.txt" "${TMP_DIR}/checksums.txt"

want="$(awk -v n="${ARCHIVE}" '$2 == n { print $1; exit }' "${TMP_DIR}/checksums.txt")"
[ -n "$want" ] || die "checksums.txt has no entry for ${ARCHIVE}"
got="$(${SHA256} "${TMP_DIR}/${ARCHIVE}" | awk '{ print $1 }')"
[ "$got" = "$want" ] || die "checksum mismatch for ${ARCHIVE}: got ${got}, want ${want}"
info "sha256 verified"

step 3 "Installing to ${INSTALL_DIR}"
mkdir -p "${INSTALL_DIR}" "${TMP_DIR}/extract"
tar -xzf "${TMP_DIR}/${ARCHIVE}" -C "${TMP_DIR}/extract"
[ -f "${TMP_DIR}/extract/pvman" ] || die "${ARCHIVE} did not contain a pvman binary"

# Replace by rename: safe even while an older pvman is running, and it never
# leaves a half-written binary where a command is expected to be.
cp "${TMP_DIR}/extract/pvman" "${BIN}.new"
chmod 0755 "${BIN}.new"
mv -f "${BIN}.new" "${BIN}"
info "wrote ${BIN}"

if [ -z "${PV_MAN_INSTALL_DIR:-}" ]; then
    case "${SHELL:-}" in
        */zsh)  RC="${HOME}/.zshrc" ;;
        */bash) RC="${HOME}/.bashrc" ;;
        *)      RC="${HOME}/.profile" ;;
    esac
    PATH_LINE='export PATH="$HOME/.local/bin:$PATH"'
    touch "${RC}"
    if ! grep -Fqx "${PATH_LINE}" "${RC}" 2>/dev/null; then
        printf '\n# pvman\n%s\n' "${PATH_LINE}" >> "${RC}"
        info "added ~/.local/bin to PATH in ${RC}"
    fi
fi

if [ "${LEGACY}" != "${BIN}" ] && [ -e "${LEGACY}" ]; then
    warn "an older pvman is still installed at ${LEGACY}"
    if [ -n "${RC}" ] && grep -Fq 'go/bin' "${RC}" 2>/dev/null; then
        # Whichever of the two PATH entries comes first in the rc file wins,
        # and the ~/.local/bin line was just appended at the end -- so the old
        # binary is still the one a bare `pvman` would run.
        info "${RC} lists ~/go/bin before ~/.local/bin, so a bare \`pvman\`"
        info "still runs the old one."
    fi
    info "remove it with: rm -f ${LEGACY}"
fi

# Older installers also pulled down a Go toolchain and a go1.26.1 wrapper.
# pvman does not use either any more, but the wording here stops short of
# claiming they are junk: on many machines ~/go/bin/go1.26.1 is the official
# golang.org/dl wrapper, which other projects may still be relying on.
leftovers=""
if [ -e "${HOME}/go/bin/go1.26.1" ]; then
    leftovers="${leftovers} ${HOME}/go/bin/go1.26.1"
fi
for d in "${HOME}"/.local/go1.*; do
    if [ -d "${d}" ]; then leftovers="${leftovers} ${d}"; fi
done
if [ -n "${leftovers}" ]; then
    warn "pvman no longer needs a Go toolchain"
    info "these are safe to remove if nothing else uses them:"
    info "rm -rf${leftovers}"
fi

printf '\n'
if [ -n "${PREV}" ] && [ "${PREV}" != "${VERSION}" ]; then
    printf 'pvman %s -> %s installed at %s\n' "${PREV}" "${VERSION}" "${BIN}"
else
    printf 'pvman %s installed at %s\n' "${VERSION}" "${BIN}"
fi
if [ -n "${RC}" ]; then
    printf 'Restart your shell, or run: source %s\n' "${RC}"
fi
