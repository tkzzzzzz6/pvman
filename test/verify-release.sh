#!/usr/bin/env bash
#
# Verify a pvman release end to end: that every archive matches the checksum
# published beside it, that it unpacks to exactly the one binary it should, and
# that the binary really is built for the platform and architecture its
# filename claims.
#
#   test/verify-release.sh v0.6.0      # fetch a published release and check it
#   test/verify-release.sh --dir dist  # check a local build before publishing
#
# The filename checks are the point. A cross-compile that silently ignored
# GOARCH still produces six plausible-looking archives of the right names, and
# only the bytes inside them tell you which is which.
#
# Needs tar, unzip, sha256sum and od. Needs no conda, uv, Go or Python.
set -euo pipefail

REPO_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)

GOOSES="linux darwin windows"
GOARCHES="amd64 arm64"

usage() {
	cat <<'EOF'
usage: test/verify-release.sh <tag>        verify the published GitHub release
       test/verify-release.sh --dir <dir>  verify archives already on disk
                                           (checksums are skipped if the
                                            directory has no checksums.txt)
EOF
}

die() {
	printf 'FAIL: %s\n' "$*" >&2
	exit 1
}

ok() { printf '  ok    %s\n' "$*"; }

# le_num <file> <offset> <nbytes> - the little-endian integer stored there.
# Every field this script reads out of a binary header is little-endian on all
# three formats we ship, because all six targets are little-endian.
le_num() {
	local file=$1 off=$2 n=$3 bytes out="" i
	bytes=$(od -An -tx1 -j"$off" -N"$n" "$file" | tr -d ' \n')
	[ "${#bytes}" -eq $((n * 2)) ] || die "cannot read $n bytes at offset $off of $file"
	for ((i = n - 1; i >= 0; i--)); do out+="${bytes:$((i * 2)):2}"; done
	printf '%d' "$((16#$out))"
}

# magic <file> - the first four bytes as hex, for identifying the container.
magic() { od -An -tx1 -N4 "$1" | tr -d ' \n'; }

# check_binary <path> <goos> <goarch> - the header must agree with the filename.
check_binary() {
	local bin=$1 goos=$2 goarch=$3 got want lfanew

	case "$goos" in
	linux)
		[ "$(magic "$bin")" = "7f454c46" ] || die "$bin is not an ELF file"
		got=$(le_num "$bin" 18 2) # e_machine
		case "$goarch" in
		amd64) want=62 ;;  # EM_X86_64   0x3e
		arm64) want=183 ;; # EM_AARCH64  0xb7
		esac
		;;
	darwin)
		[ "$(magic "$bin")" = "cffaedfe" ] || die "$bin is not a 64-bit Mach-O file"
		got=$(le_num "$bin" 4 4) # cputype
		case "$goarch" in
		amd64) want=16777223 ;; # CPU_TYPE_X86_64  0x01000007
		arm64) want=16777228 ;; # CPU_TYPE_ARM64   0x0100000c
		esac
		;;
	windows)
		[ "$(magic "$bin" | cut -c1-4)" = "4d5a" ] || die "$bin is not a PE file"
		lfanew=$(le_num "$bin" 60 4)      # e_lfanew, points at the PE header
		got=$(le_num "$bin" $((lfanew + 4)) 2) # Machine
		case "$goarch" in
		amd64) want=34404 ;; # IMAGE_FILE_MACHINE_AMD64 0x8664
		arm64) want=43620 ;; # IMAGE_FILE_MACHINE_ARM64 0xaa64
		esac
		;;
	*)
		die "unknown GOOS $goos"
		;;
	esac

	[ "$got" = "$want" ] ||
		die "$(basename "$bin"): header machine is $got, filename claims $goarch ($want)"
	ok "$(basename "$bin") is really $goos/$goarch"
}

# verify_archive <archive> <version> - unpack and inspect one archive.
verify_archive() {
	local archive=$1 version=$2
	local base stem rest goos goarch bin tmp entries

	base=$(basename "$archive")
	stem=${base%.tar.gz}
	stem=${stem%.zip}

	goarch=${stem##*_}
	rest=${stem%_*}
	goos=${rest##*_}

	[ "$goos" = linux ] || [ "$goos" = darwin ] || [ "$goos" = windows ] ||
		die "$base: cannot read a platform out of the filename"
	[ "$goarch" = amd64 ] || [ "$goarch" = arm64 ] ||
		die "$base: cannot read an architecture out of the filename"
	[ "${rest%_*}" = "pvman_${version}" ] ||
		die "$base: expected version ${version}, filename says otherwise"

	tmp=$(mktemp -d)
	case "$base" in
	*.tar.gz) tar -xzf "$archive" -C "$tmp" ;;
	*.zip) unzip -qq -o "$archive" -d "$tmp" ;;
	esac

	# A stray directory or a second file would make `tar -xzf` in the install
	# instructions produce something other than a runnable pvman.
	entries=$(find "$tmp" -mindepth 1 -maxdepth 1 | wc -l | tr -d ' ')
	[ "$entries" -eq 1 ] || die "$base should hold exactly one entry, found $entries"

	if [ "$goos" = windows ]; then bin=$tmp/pvman.exe; else bin=$tmp/pvman; fi
	[ -f "$bin" ] || die "$base does not contain $(basename "$bin")"

	check_binary "$bin" "$goos" "$goarch"
	rm -rf "$tmp"
}

main() {
	local dir="" tag=""

	while [ $# -gt 0 ]; do
		case "$1" in
		--dir)
			dir=${2:-}
			[ -n "$dir" ] || die "--dir needs a path"
			shift 2
			;;
		-h | --help)
			usage
			exit 0
			;;
		-*)
			die "unknown option $1"
			;;
		*)
			tag=$1
			shift
			;;
		esac
	done

	local work
	if [ -n "$dir" ]; then
		[ -d "$dir" ] || die "no such directory: $dir"
		work=$(cd "$dir" && pwd)
		printf 'Verifying local build in %s\n' "$work"
	else
		[ -n "$tag" ] || {
			usage
			exit 2
		}
		command -v gh >/dev/null || die "--dir is required when gh is not installed"
		work=$(mktemp -d)
		# Expanded now, not at exit: `work` is local to main and is already out
		# of scope by the time an EXIT trap runs.
		trap "rm -rf '$work'" EXIT
		printf 'Downloading release %s\n' "$tag"
		# Run from the checkout: gh reads the repository out of the git remote,
		# and the scratch directory is not one.
		(cd "$REPO_ROOT" && gh release download "$tag" --dir "$work" --clobber) ||
			die "could not download release $tag"
	fi

	printf '\nChecksums\n'
	if [ -f "$work/checksums.txt" ]; then
		(cd "$work" && sha256sum -c checksums.txt) || die "a checksum did not match"
	elif [ -n "$dir" ]; then
		# A hand-built directory has no checksums.txt unless the release steps
		# were run over it. Saying so beats refusing to look at the archives,
		# which is what --dir is actually for.
		printf '  skip  no checksums.txt here; checking the archives on their own\n'
	else
		# A published release always carries one, so its absence is a defect.
		die "release $tag has no checksums.txt"
	fi

	printf '\nArchives\n'
	local archives=()
	while IFS= read -r f; do archives+=("$f"); done < <(
		find "$work" -maxdepth 1 \( -name '*.tar.gz' -o -name '*.zip' \) | sort
	)
	[ "${#archives[@]}" -gt 0 ] || die "no archives found in $work"

	# Every archive carries the same version, so the first one names it.
	local version=${archives[0]##*/}
	version=${version#pvman_}
	version=${version%%_*}

	for a in "${archives[@]}"; do
		verify_archive "$a" "$version"
	done

	printf '\nCoverage\n'
	local goos goarch
	for goos in $GOOSES; do
		for goarch in $GOARCHES; do
			if [ -f "$work/pvman_${version}_${goos}_${goarch}.tar.gz" ] ||
				[ -f "$work/pvman_${version}_${goos}_${goarch}.zip" ]; then
				continue
			fi
			die "no archive for $goos/$goarch"
		done
	done
	ok "all 6 platform/architecture archives present"

	printf '\nPASS: %s verified\n' "${tag:-$work}"
}

main "$@"
