// Package update implements `pvman --update`. It works out how the running
// binary was installed and refreshes it through that same channel: `go
// install` for binaries the install scripts placed in the Go bin directory,
// a verified download from the GitHub release for everything else.
package update

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const repo = "tkzzzzzz6/pvman"

const apiBase = "https://api.github.com"

var client = &http.Client{Timeout: 90 * time.Second}

// fetchLatestFn is a seam for tests: Run goes through it instead of calling
// the real GitHub API.
var fetchLatestFn = fetchLatest

// Channel identifies how the running binary got installed.
type Channel int

const (
	// ChannelBinary is a binary unpacked from a release archive, living
	// wherever the user put it.
	ChannelBinary Channel = iota
	// ChannelGoInstall is a binary `go install` dropped into the Go bin
	// directory, refreshed with `go install ...@latest`.
	ChannelGoInstall
)

func (c Channel) String() string {
	switch c {
	case ChannelGoInstall:
		return "go install"
	default:
		return "release binary"
	}
}

type asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type release struct {
	TagName string  `json:"tag_name"`
	Assets  []asset `json:"assets"`
}

// Run updates pvman through whichever channel installed it, printing progress
// to out. injected is main.version ("" unless set via -ldflags at release
// time); from there EffectiveVersion recovers the real version.
func Run(injected string, out io.Writer) error {
	current := EffectiveVersion(injected)
	if current == "dev" {
		return errors.New("this pvman was built from source and cannot update itself; instead run:\n  go install github.com/" + repo + "@latest")
	}
	fmt.Fprintf(out, "current version: %s\n", current)

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate running executable: %w", err)
	}
	channel := DetectChannel(exe)

	rel, err := fetchLatestFn()
	if err != nil {
		// The go-install channel does not need the GitHub API -- `go install
		// @latest` resolves the newest version itself -- so a rate-limited or
		// unreachable API must not block it. Release binaries have no such
		// fallback: they need the API to learn the asset URLs and checksums.
		if channel == ChannelGoInstall {
			fmt.Fprintf(out, "could not check GitHub for the latest release (%v); trying go install anyway\n", err)
			return updateViaGo(out, exe)
		}
		return fmt.Errorf("%w\nGitHub rate-limits unauthenticated update checks; try again later, or run:\n  go install github.com/%s@latest", err, repo)
	}
	latest := strings.TrimPrefix(rel.TagName, "v")

	if CompareVersions(current, latest) >= 0 {
		fmt.Fprintf(out, "pvman %s is already up to date\n", current)
		return nil
	}
	fmt.Fprintf(out, "new release: %s (installed via %s)\n", rel.TagName, channel)

	switch channel {
	case ChannelGoInstall:
		return updateViaGo(out, exe)
	default:
		return updateViaRelease(rel, latest, exe, out)
	}
}

func fetchLatest() (*release, error) {
	req, err := http.NewRequest(http.MethodGet, apiBase+"/repos/"+repo+"/releases/latest", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "pvman")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return nil, fmt.Errorf("GitHub API returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var rel release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("decode GitHub release: %w", err)
	}
	if rel.TagName == "" {
		return nil, errors.New("GitHub API response carried no tag_name")
	}
	return &rel, nil
}

// DetectChannel reports how the executable at exe was installed.
func DetectChannel(exe string) Channel {
	return detectChannel(exe, goBinDirs())
}

func detectChannel(exe string, bins []string) Channel {
	dir := filepath.Dir(exe)
	for _, b := range bins {
		if strings.EqualFold(filepath.Clean(dir), filepath.Clean(b)) {
			return ChannelGoInstall
		}
	}
	return ChannelBinary
}

func goBinDirs() []string {
	var dirs []string
	if gb := os.Getenv("GOBIN"); gb != "" {
		dirs = append(dirs, gb)
	}
	gp := os.Getenv("GOPATH")
	if gp == "" {
		if home, err := os.UserHomeDir(); err == nil {
			gp = filepath.Join(home, "go")
		}
	}
	for _, p := range filepath.SplitList(gp) {
		if p != "" {
			dirs = append(dirs, filepath.Join(p, "bin"))
		}
	}
	return dirs
}

func updateViaGo(out io.Writer, exe string) error {
	goCmd := findManagedGo()
	if goCmd == "" {
		p, err := exec.LookPath("go")
		if err != nil {
			return errors.New("no Go toolchain found: looked for pvman's managed SDK and `go` on PATH")
		}
		goCmd = p
	}
	fmt.Fprintf(out, "installing github.com/%s@latest via %s\n", repo, goCmd)

	install := exec.Command(goCmd, "install", "github.com/"+repo+"@latest")
	install.Stdout = out
	install.Stderr = out

	if runtime.GOOS != "windows" {
		if err := install.Run(); err != nil {
			return fmt.Errorf("go install failed: %w", err)
		}
		// The new binary is in place; confirm what we just got.
		if b, err := exec.Command(exe, "--version").Output(); err == nil {
			fmt.Fprintf(out, "now running: %s\n", strings.TrimSpace(string(b)))
		}
		return nil
	}

	// Windows cannot replace a running executable, so install into a staging
	// directory and swap once this process exits.
	stage, err := os.MkdirTemp("", "pvman-update-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	install.Env = append(os.Environ(), "GOBIN="+stage)
	if err := install.Run(); err != nil {
		return fmt.Errorf("go install failed: %w", err)
	}
	staged := filepath.Join(stage, "pvman.exe")
	if _, err := os.Stat(staged); err != nil {
		return fmt.Errorf("go install produced no binary at %s", staged)
	}
	return replaceRunning(staged, exe, out)
}

// findManagedGo returns the go binary of the SDK the pvman install scripts
// downloaded from go.dev, if that SDK is still on disk.
func findManagedGo() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	var roots []string
	if runtime.GOOS == "windows" {
		la := os.Getenv("LOCALAPPDATA")
		if la == "" {
			la = filepath.Join(home, "AppData", "Local")
		}
		roots = append(roots, filepath.Join(la, "pvman"))
	} else {
		roots = append(roots, filepath.Join(home, ".local"))
	}
	return findManagedGoIn(roots)
}

func findManagedGoIn(roots []string) string {
	name := "go"
	if runtime.GOOS == "windows" {
		name = "go.exe"
	}
	var candidates []string
	for _, root := range roots {
		matches, _ := filepath.Glob(filepath.Join(root, "go1.*", "bin", name))
		candidates = append(candidates, matches...)
	}
	if len(candidates) == 0 {
		return ""
	}
	sort.Strings(candidates)
	return candidates[len(candidates)-1]
}

func updateViaRelease(rel *release, latest, exe string, out io.Writer) error {
	goos, goarch := runtime.GOOS, runtime.GOARCH
	binName := "pvman"
	archiveName := fmt.Sprintf("pvman_%s_%s_%s.tar.gz", latest, goos, goarch)
	if goos == "windows" {
		binName = "pvman.exe"
		archiveName = fmt.Sprintf("pvman_%s_%s_%s.zip", latest, goos, goarch)
	}

	assetURL := findAsset(rel, archiveName)
	sumsURL := findAsset(rel, "checksums.txt")
	if assetURL == "" || sumsURL == "" {
		return fmt.Errorf("release %s is missing %s or checksums.txt", rel.TagName, archiveName)
	}

	work, err := os.MkdirTemp("", "pvman-update-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)

	archivePath := filepath.Join(work, archiveName)
	if err := downloadFile(assetURL, archivePath, out); err != nil {
		return err
	}
	if err := downloadFile(sumsURL, filepath.Join(work, "checksums.txt"), out); err != nil {
		return err
	}
	if err := verifyChecksum(work, archiveName); err != nil {
		return err
	}
	fmt.Fprintln(out, "checksum verified")

	newBin, err := extractBinary(archivePath, work, binName)
	if err != nil {
		return err
	}
	return replaceRunning(newBin, exe, out)
}

func findAsset(rel *release, name string) string {
	for _, a := range rel.Assets {
		if a.Name == name {
			return a.BrowserDownloadURL
		}
	}
	return ""
}

func downloadFile(url, dest string, out io.Writer) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "pvman")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: %s", filepath.Base(dest), resp.Status)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	n, err := io.Copy(f, resp.Body)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "  downloaded %s (%.1f MB)\n", filepath.Base(dest), float64(n)/1e6)
	return nil
}

func verifyChecksum(dir, archiveName string) error {
	sums, err := os.ReadFile(filepath.Join(dir, "checksums.txt"))
	if err != nil {
		return err
	}
	want := ""
	for _, line := range strings.Split(string(sums), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == archiveName {
			want = fields[0]
			break
		}
	}
	if want == "" {
		return fmt.Errorf("checksums.txt has no entry for %s", archiveName)
	}
	data, err := os.ReadFile(filepath.Join(dir, archiveName))
	if err != nil {
		return err
	}
	got := sha256.Sum256(data)
	if !strings.EqualFold(hex.EncodeToString(got[:]), want) {
		return fmt.Errorf("checksum mismatch for %s: got %x, want %s", archiveName, got, want)
	}
	return nil
}

func extractBinary(archivePath, destDir, binName string) (string, error) {
	dest := filepath.Join(destDir, binName)
	if strings.HasSuffix(archivePath, ".zip") {
		r, err := zip.OpenReader(archivePath)
		if err != nil {
			return "", err
		}
		defer r.Close()
		for _, f := range r.File {
			if filepath.Base(f.Name) != binName {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				return "", err
			}
			err = writeFile(dest, rc)
			rc.Close()
			if err != nil {
				return "", err
			}
			return dest, nil
		}
		return "", fmt.Errorf("archive has no %s", binName)
	}

	f, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", err
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		if filepath.Base(hdr.Name) == binName {
			if err := writeFile(dest, tr); err != nil {
				return "", err
			}
			return dest, nil
		}
	}
	return "", fmt.Errorf("archive has no %s", binName)
}

func writeFile(dest string, src io.Reader) error {
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, src)
	return err
}
