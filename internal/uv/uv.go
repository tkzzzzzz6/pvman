package uv

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

type Env struct {
	Name      string
	Path      string
	Active    bool
	PythonVer string
	PkgCount  int
	SizeBytes int64
	CreatedAt time.Time // zero when it cannot be determined
	Loaded    bool
}

// ScanDir finds all uv venvs in the given directory (one level deep).
func ScanDir(dir string) ([]Env, error) {
	activeEnv, _ := filepath.Abs(os.Getenv("VIRTUAL_ENV"))

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var envs []Env
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		envPath := filepath.Join(dir, e.Name())
		if _, err := os.Stat(filepath.Join(envPath, "pyvenv.cfg")); err == nil {
			absPath, _ := filepath.Abs(envPath)
			env := Env{
				Name:   e.Name(),
				Path:   envPath,
				Active: absPath == activeEnv,
			}
			envs = append(envs, env)
		}
	}
	return envs, nil
}

func LoadDetails(env *Env) {
	env.PythonVer = getPythonVersionFromCfg(env.Path)
	env.PkgCount = getPkgCount(env.Path)
	env.SizeBytes = getDirSize(env.Path)
	env.CreatedAt = getCreatedAt(env.Path)
	env.Loaded = true
}

// getCreatedAt reports when the venv was created, taken from pyvenv.cfg, which
// is written once when the venv is made and not touched by installing packages.
func getCreatedAt(envPath string) time.Time {
	info, err := os.Stat(filepath.Join(envPath, "pyvenv.cfg"))
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

func getPythonVersionFromCfg(envPath string) string {
	f, err := os.Open(filepath.Join(envPath, "pyvenv.cfg"))
	if err != nil {
		return "unknown"
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "version") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return "unknown"
}

func getPkgCount(envPath string) int {
	cmd := exec.Command("uv", "pip", "list", "--python", pythonBinary(envPath))
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) <= 2 {
		return 0
	}
	return len(lines) - 2
}

func pythonBinary(envPath string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(envPath, "Scripts", "python.exe")
	}
	return filepath.Join(envPath, "bin", "python")
}

func getDirSize(path string) int64 {
	var size int64
	_ = filepath.WalkDir(path, func(_ string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		size += info.Size()
		return nil
	})
	return size
}

func CreateEnv(dir, name, pythonVer string) error {
	args := []string{"venv", filepath.Join(dir, name)}
	if pythonVer != "" {
		args = append(args, "--python", pythonVer)
	}
	cmd := exec.Command("uv", args...)
	return cmd.Run()
}

func DeleteEnv(env Env) error {
	return os.RemoveAll(env.Path)
}

// ListPackages returns the sorted, de-duplicated package names in a venv.
func ListPackages(env Env) ([]string, error) {
	py := pythonBinary(env.Path)

	// Try JSON first (modern uv supports --format=json).
	// uv writes its "Using Python ..." banner and every diagnostic to stderr,
	// so both streams have to be captured for an error to say anything useful.
	var stderr bytes.Buffer
	cmd := exec.Command("uv", "pip", "list", "--python", py, "--format=json")
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err == nil {
		var pkgs []struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(out, &pkgs) == nil {
			raw := make([]string, 0, len(pkgs))
			for _, p := range pkgs {
				raw = append(raw, p.Name)
			}
			return dedupeSorted(raw), nil
		}
	}

	// Fallback to plain text parsing.
	stderr.Reset()
	cmd = exec.Command("uv", "pip", "list", "--python", py)
	cmd.Stderr = &stderr
	out, err = cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%v: %s", err, firstLine(stderr.Bytes()))
	}
	var raw []string
	for i, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if i < 2 {
			continue // skip the "Package Version" / "------- -------" header
		}
		if fields := strings.Fields(strings.TrimSpace(line)); len(fields) > 0 {
			raw = append(raw, fields[0])
		}
	}
	return dedupeSorted(raw), nil
}

func dedupeSorted(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, n := range in {
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// depsScript asks the environment's own interpreter for every installed
// distribution and its declared requirements. importlib.metadata resolves the
// site-packages layout for us, so no path guessing is needed.
const depsScript = `import importlib.metadata as md, json, sys
out = []
for d in md.distributions():
    try:
        name = d.metadata.get("Name") or ""
        reqs = list(d.requires or [])
    except Exception:
        continue
    if name:
        out.append({"n": name, "r": reqs})
sys.stdout.write(json.dumps(out))`

// Dependencies returns both directions of a venv's dependency relation, keyed
// by the same names ListPackages reports.
//
// deps[p] is what p needs; dependents[p] is what needs p. Only edges between
// installed distributions are kept, so an optional or absent dependency never
// shows up as an actionable entry.
func Dependencies(env Env) (deps, dependents map[string][]string, err error) {
	var stderr bytes.Buffer
	cmd := exec.Command(pythonBinary(env.Path), "-c", depsScript)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, nil, fmt.Errorf("%v: %s", err, firstLine(stderr.Bytes()))
	}

	var dists []struct {
		Name     string   `json:"n"`
		Requires []string `json:"r"`
	}
	if err := json.Unmarshal(out, &dists); err != nil {
		return nil, nil, err
	}

	// Requirements are written in the PEP 503 spelling, which need not match the
	// installed distribution's display name, so resolve through an index.
	display := make(map[string]string, len(dists))
	for _, d := range dists {
		if k := normalizeName(d.Name); k != "" {
			display[k] = d.Name
		}
	}

	deps = make(map[string][]string, len(dists))
	for _, d := range dists {
		key := display[normalizeName(d.Name)]
		if key == "" {
			continue
		}
		var names []string
		for _, req := range d.Requires {
			n, ok := display[reqName(req)]
			if !ok {
				continue // optional, conditional, or not installed
			}
			names = append(names, n)
		}
		deps[key] = dedupeSorted(names)
	}
	return deps, invert(deps), nil
}

// reqName extracts the distribution name from a Requires-Dist entry such as
// "numpy (<2.0.0,>=1.17)". Extras are only installed on request, so a
// requirement guarded by one is not a real dependency of an installed package.
func reqName(req string) string {
	if i := strings.IndexByte(req, ';'); i >= 0 {
		if strings.Contains(req[i:], "extra") {
			return ""
		}
		req = req[:i]
	}
	if i := strings.IndexAny(req, " \t([<>=!~"); i >= 0 {
		req = req[:i]
	}
	return normalizeName(req)
}

// normalizeName applies the PEP 503 name normalisation, so a requirement on
// "charset_normalizer" matches the installed "charset-normalizer".
func normalizeName(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch r {
		case '-', '_', '.':
			if !dash {
				b.WriteByte('-')
				dash = true
			}
		default:
			dash = false
			b.WriteRune(r)
		}
	}
	return strings.Trim(b.String(), "-")
}

// invert turns "depends on" into "is depended on by".
func invert(deps map[string][]string) map[string][]string {
	out := make(map[string][]string, len(deps))
	for pkg, names := range deps {
		for _, n := range names {
			out[n] = append(out[n], pkg)
		}
	}
	for k, v := range out {
		out[k] = dedupeSorted(v)
	}
	return out
}

// RemovePackage removes packages from a venv and reports how many went away.
//
// Unlike conda, pip and uv uninstall exactly what they are given and leave
// dependents in place, so the count is simply the number of names passed in.
func RemovePackage(env Env, pkgs ...string) (int, error) {
	if len(pkgs) == 0 {
		return 0, nil
	}
	py := pythonBinary(env.Path)
	args := append([]string{"pip", "uninstall", "--python", py}, pkgs...)
	// uv does not prompt, but capture stderr so failures are reported usefully.
	if out, err := exec.Command("uv", args...).CombinedOutput(); err != nil {
		return 0, fmt.Errorf("%v: %s", err, firstLine(out))
	}
	return len(pkgs), nil
}

// firstLine keeps CLI error output to one readable line for the status bar.
func firstLine(b []byte) string {
	s := strings.TrimSpace(string(b))
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

func ActivateCmd(env Env) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(env.Path, "Scripts", "activate.bat")
	}
	return "source " + fmt.Sprintf("%q", filepath.Join(env.Path, "bin", "activate"))
}
