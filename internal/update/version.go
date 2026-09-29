package update

import (
	"runtime/debug"
	"strconv"
	"strings"
)

// EffectiveVersion reports the running binary's version. Release builds have
// it injected via -ldflags "-X main.version=..."; `go install` builds recover
// the release tag from the build info the toolchain recorded. Anything else
// -- a plain `go build` from a checkout, which the toolchain stamps with a
// pseudo-version like v0.6.2-0.20260928151605-ae9df729c436+dirty -- reports
// as "dev".
func EffectiveVersion(injected string) string {
	if injected != "" {
		return strings.TrimPrefix(injected, "v")
	}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	if !isReleaseVersion(bi.Main.Version) {
		return "dev"
	}
	return strings.TrimPrefix(bi.Main.Version, "v")
}

// isReleaseVersion reports whether v looks like a released version (v0.6.1,
// v0.7.0-rc1) rather than a pseudo-version from building a VCS checkout
// (v0.6.2-0.2026...+dirty, v0.0.0-20240101000000-abcdef). Only the former
// should drive update decisions.
func isReleaseVersion(v string) bool {
	if v == "" || v == "(devel)" {
		return false
	}
	i := strings.IndexByte(v, '-')
	if i < 0 {
		return true
	}
	// vX.Y.Z-0.TIMESTAMP-HASH: the "0." segment only appears in
	// pseudo-versions derived from a preceding tag.
	if strings.HasPrefix(v[i+1:], "0.") {
		return false
	}
	// v0.0.0-TIMESTAMP-HASH: a timestamp plus commit, as tagged by a repo
	// with no release tags of its own.
	pre := v[i+1:]
	if j := strings.IndexByte(pre, '-'); j >= 12 && allDigits(pre[:j]) {
		return false
	}
	return true
}

func allDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

type semver struct {
	major, minor, patch int
	pre                 []string
	hasPre              bool
}

func parseVersion(s string) (semver, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	var v semver
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i] // build metadata never affects precedence
	}
	if i := strings.IndexByte(s, '-'); i >= 0 {
		v.hasPre = true
		v.pre = strings.Split(s[i+1:], ".")
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return semver{}, false
	}
	nums := []*int{&v.major, &v.minor, &v.patch}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return semver{}, false
		}
		*nums[i] = n
	}
	return v, true
}

// CompareVersions compares two version strings; a leading "v" is optional.
// It returns -1 if a < b, 0 if they are equivalent, +1 if a > b. Unparseable
// input sorts before anything parseable, so a dev build always counts as
// older than every release -- which is the safe direction for an updater.
func CompareVersions(a, b string) int {
	va, oka := parseVersion(a)
	vb, okb := parseVersion(b)
	if !oka || !okb {
		switch {
		case !oka && !okb:
			return 0
		case !oka:
			return -1
		default:
			return 1
		}
	}
	if c := cmpInt(va.major, vb.major); c != 0 {
		return c
	}
	if c := cmpInt(va.minor, vb.minor); c != 0 {
		return c
	}
	if c := cmpInt(va.patch, vb.patch); c != 0 {
		return c
	}
	return comparePrerelease(va, vb)
}

// comparePrerelease implements semver 11.4.4: a release outranks any of its
// prereleases, and prereleases compare field by field.
func comparePrerelease(a, b semver) int {
	if !a.hasPre && !b.hasPre {
		return 0
	}
	if !a.hasPre {
		return 1
	}
	if !b.hasPre {
		return -1
	}
	for i := 0; i < len(a.pre) && i < len(b.pre); i++ {
		if c := compareIdent(a.pre[i], b.pre[i]); c != 0 {
			return c
		}
	}
	return cmpInt(len(a.pre), len(b.pre))
}

// compareIdent compares one prerelease field: numeric identifiers compare
// numerically and rank below alphanumeric ones, which compare lexically.
func compareIdent(a, b string) int {
	an, aerr := strconv.Atoi(a)
	bn, berr := strconv.Atoi(b)
	switch {
	case aerr == nil && berr == nil:
		return cmpInt(an, bn)
	case aerr == nil:
		return -1
	case berr == nil:
		return 1
	default:
		return strings.Compare(a, b)
	}
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}
