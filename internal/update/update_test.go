package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.6.0", "0.6.0", 0},
		{"v0.6.0", "0.6.0", 0},
		{"0.6.0", "0.6.1", -1},
		{"0.6.1", "0.6.0", 1},
		{"0.9.0", "0.10.0", -1}, // numeric, not lexical
		{"0.10.0", "0.9.0", 1},
		{"0.7.0", "0.7.0-rc1", 1}, // a release outranks its prereleases
		{"0.7.0-rc1", "0.7.0", -1},
		{"0.7.0-rc1", "0.7.0-rc2", -1},
		{"0.7.0-rc2", "0.7.0-rc1", 1},
		{"0.7.0-rc1", "0.7.0-rc1.1", -1}, // more fields rank higher
		{"1.0.0-1", "1.0.0-alpha", -1},   // numeric identifiers rank below alpha
		{"1.0.0-alpha", "1.0.0-beta", -1},
		{"1.0.0-alpha+build5", "1.0.0-alpha+build9", 0}, // build metadata ignored
		{"dev", "0.6.0", -1},                            // unparseable sorts first
		{"0.6.0", "dev", 1},
		{"dev", "dev", 0},
		{"0.6", "0.6.0", -1}, // malformed counts as unparseable
	}
	for _, c := range cases {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestEffectiveVersion(t *testing.T) {
	if got := EffectiveVersion("0.7.0"); got != "0.7.0" {
		t.Errorf("injected plain: got %q", got)
	}
	if got := EffectiveVersion("v0.7.0"); got != "0.7.0" {
		t.Errorf("injected with v should be stripped: got %q", got)
	}
	// `go test` builds the test binary from local source without VCS stamping,
	// so the build info carries no release version and this falls through to
	// "dev".
	if got := EffectiveVersion(""); got != "dev" {
		t.Errorf("no injection should report dev: got %q", got)
	}
}

func TestIsReleaseVersion(t *testing.T) {
	cases := []struct {
		v    string
		want bool
	}{
		{"v0.6.1", true},
		{"v0.7.0-rc1", true}, // a real prerelease tag is still a release
		{"", false},
		{"(devel)", false},
		{"v0.6.2-0.20260928151605-ae9df729c436+dirty", false}, // dirty source build
		{"v0.6.2-0.20260928151605-ae9df729c436", false},       // ahead of the last tag
		{"v0.0.0-20240101000000-abcdefabcdef", false},         // repo without tags
	}
	for _, c := range cases {
		if got := isReleaseVersion(c.v); got != c.want {
			t.Errorf("isReleaseVersion(%q) = %v, want %v", c.v, got, c.want)
		}
	}
}

func TestDetectChannel(t *testing.T) {
	type channelCase struct {
		exe  string
		bins []string
		want Channel
	}
	cases := []channelCase{
		{"/home/u/go/bin/pvman", []string{"/home/u/go/bin"}, ChannelGoInstall},
		{"/opt/bin/pvman", []string{"/home/u/go/bin"}, ChannelBinary},
		{"/home/u/go/bin/pvman", []string{"/home/u/go/bin-extra"}, ChannelBinary}, // no prefix matching
		{"/home/u/go/bin/pvman", nil, ChannelBinary},
	}
	if runtime.GOOS == "windows" {
		// Backslash-separated paths only parse as directories on Windows;
		// on other platforms filepath treats them as one opaque name, so
		// these cases belong to the Windows runner only.
		cases = append(cases,
			channelCase{`C:\Users\u\go\bin\pvman.exe`, []string{`C:\Users\u\go\bin`}, ChannelGoInstall},
			channelCase{`C:\Users\u\go\bin\pvman.exe`, []string{`c:\users\u\go\bin`}, ChannelGoInstall}, // case-insensitive
		)
	}
	for _, c := range cases {
		if got := detectChannel(c.exe, c.bins); got != c.want {
			t.Errorf("detectChannel(%q, %v) = %v, want %v", c.exe, c.bins, got, c.want)
		}
	}
}

func TestFindManagedGoIn(t *testing.T) {
	root := t.TempDir()
	sdk := filepath.Join(root, "go1.26.1", "bin")
	if err := os.MkdirAll(sdk, 0o755); err != nil {
		t.Fatal(err)
	}
	name := "go"
	if runtime.GOOS == "windows" {
		name = "go.exe"
	}
	if err := os.WriteFile(filepath.Join(sdk, name), []byte("fake"), 0o755); err != nil {
		t.Fatal(err)
	}

	if got := findManagedGoIn([]string{root}); got == "" {
		t.Fatal("expected to find the managed SDK")
	}
	if got := findManagedGoIn([]string{filepath.Join(root, "nowhere")}); got != "" {
		t.Errorf("expected no SDK, got %q", got)
	}
}

func TestVerifyChecksum(t *testing.T) {
	dir := t.TempDir()
	payload := []byte("fake binary bytes")
	sum := sha256.Sum256(payload)
	name := "pvman_0.7.0_linux_amd64.tar.gz"
	os.WriteFile(filepath.Join(dir, name), payload, 0o644)
	os.WriteFile(filepath.Join(dir, "checksums.txt"), []byte(fmt.Sprintf("%x  %s\n", sum, name)), 0o644)

	if err := verifyChecksum(dir, name); err != nil {
		t.Fatalf("valid checksum rejected: %v", err)
	}

	os.WriteFile(filepath.Join(dir, name), []byte("tampered"), 0o644)
	if err := verifyChecksum(dir, name); err == nil {
		t.Fatal("tampered payload passed verification")
	}

	if err := verifyChecksum(dir, "nope.tar.gz"); err == nil {
		t.Fatal("missing checksum entry passed verification")
	}
}

// makeArchive returns the archive bytes for the current platform in the same
// format the release workflow publishes.
func makeArchive(t *testing.T, binName string, payload []byte) (archive []byte, archiveName string) {
	t.Helper()
	latest := "0.7.0"
	if runtime.GOOS == "windows" {
		archiveName = fmt.Sprintf("pvman_%s_%s_%s.zip", latest, runtime.GOOS, runtime.GOARCH)
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		w, err := zw.Create(binName)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(payload); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes(), archiveName
	}
	archiveName = fmt.Sprintf("pvman_%s_%s_%s.tar.gz", latest, runtime.GOOS, runtime.GOARCH)
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: binName, Mode: 0o755, Size: int64(len(payload))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes(), archiveName
}

func TestUpdateViaRelease(t *testing.T) {
	binName := "pvman"
	if runtime.GOOS == "windows" {
		binName = "pvman.exe"
	}
	payload := []byte("#!/bin/sh\necho pvman 0.7.0\n")
	archive, archiveName := makeArchive(t, binName, payload)
	sum := sha256.Sum256(archive)

	mux := http.NewServeMux()
	base := "/tkzzzzzz6/pvman/releases/download/v0.7.0"
	mux.HandleFunc(base+"/"+archiveName, func(w http.ResponseWriter, r *http.Request) {
		w.Write(archive)
	})
	mux.HandleFunc(base+"/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%x  %s\n", sum, archiveName)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	rel := &release{
		TagName: "v0.7.0",
		Assets: []asset{
			{Name: archiveName, BrowserDownloadURL: server.URL + base + "/" + archiveName},
			{Name: "checksums.txt", BrowserDownloadURL: server.URL + base + "/checksums.txt"},
		},
	}

	dir := t.TempDir()
	exe := filepath.Join(dir, binName)
	os.WriteFile(exe, []byte("old pvman"), 0o755)

	var out bytes.Buffer
	if err := updateViaRelease(rel, "0.7.0", exe, &out); err != nil {
		t.Fatalf("updateViaRelease: %v\noutput:\n%s", err, out.String())
	}
	t.Logf("updater output:\n%s", out.String())

	if runtime.GOOS == "windows" {
		// The running executable cannot be swapped on Windows; the helper is
		// spawned and the new binary staged beside the target.
		staged, err := os.ReadFile(exe + ".new")
		if err != nil {
			t.Fatalf("staged binary missing: %v", err)
		}
		if !bytes.Equal(staged, payload) {
			t.Fatal("staged binary content mismatch")
		}
	} else {
		got, err := os.ReadFile(exe)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, payload) {
			t.Fatal("target was not replaced with the new binary")
		}
	}
}

func TestRunRefusesSourceBuild(t *testing.T) {
	// The test binary is built from source, so EffectiveVersion reports dev
	// and Run must refuse before touching the network or the filesystem.
	var out bytes.Buffer
	if err := Run("", &out); err == nil {
		t.Fatal("source build should not self-update")
	}
}

func TestRunUpToDate(t *testing.T) {
	old := fetchLatestFn
	defer func() { fetchLatestFn = old }()
	fetchLatestFn = func() (*release, error) {
		return &release{TagName: "v0.6.0"}, nil
	}

	var out bytes.Buffer
	if err := Run("0.6.0", &out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out.String(), "already up to date") {
		t.Errorf("expected up-to-date message, got:\n%s", out.String())
	}
}

func TestUpdateViaReleaseTampered(t *testing.T) {
	// A release whose checksums.txt lies about the archive must be rejected.
	binName := "pvman"
	if runtime.GOOS == "windows" {
		binName = "pvman.exe"
	}
	archive, archiveName := makeArchive(t, binName, []byte("good"))

	mux := http.NewServeMux()
	base := "/tkzzzzzz6/pvman/releases/download/v0.7.0"
	mux.HandleFunc(base+"/"+archiveName, func(w http.ResponseWriter, r *http.Request) {
		w.Write(archive)
	})
	mux.HandleFunc(base+"/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%x  %s\n", sha256.Sum256([]byte("different bytes")), archiveName)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	rel := &release{
		TagName: "v0.7.0",
		Assets: []asset{
			{Name: archiveName, BrowserDownloadURL: server.URL + base + "/" + archiveName},
			{Name: "checksums.txt", BrowserDownloadURL: server.URL + base + "/checksums.txt"},
		},
	}

	dir := t.TempDir()
	exe := filepath.Join(dir, binName)
	os.WriteFile(exe, []byte("old pvman"), 0o755)

	err := updateViaRelease(rel, "0.7.0", exe, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected checksum mismatch, got %v", err)
	}
}
