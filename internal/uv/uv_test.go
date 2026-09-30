package uv

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestNormalizeName(t *testing.T) {
	// PEP 503: lowercase, with runs of -, _ and . collapsed to a single dash.
	cases := map[string]string{
		"charset_normalizer":  "charset-normalizer",
		"Charset-Normalizer":  "charset-normalizer",
		"zope.interface":      "zope-interface",
		"typing_extensions":   "typing-extensions",
		"ruamel.yaml.clib":    "ruamel-yaml-clib",
		"tensorflow--gpu":     "tensorflow-gpu",
		"  Flask  ":           "flask",
		"-leading-and-trail-": "leading-and-trail",
		"":                    "",
	}
	for in, want := range cases {
		if got := normalizeName(in); got != want {
			t.Errorf("normalizeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestReqName(t *testing.T) {
	cases := map[string]string{
		"numpy (<2.0.0,>=1.17)":                   "numpy",
		"packaging (>=20.0)":                      "packaging",
		"psutil":                                  "psutil",
		"charset_normalizer (<4,>=2)":             "charset-normalizer",
		"numpy (>=1.17) ; python_version < \"3\"": "numpy",
		// Optional extras are not installed on request of the dependency, so
		// they are not real dependencies of an installed distribution.
		"deepspeed (<=0.14.0) ; extra == 'deepspeed'": "",
		"sphinx ; extra == \"docs\"":                  "",
		"":                                            "",
	}
	for in, want := range cases {
		if got := reqName(in); got != want {
			t.Errorf("reqName(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestDependenciesOverScriptOutput exercises the parsing that sits between the
// interpreter's JSON and the two maps, without spawning python.
func TestDependenciesOverScriptOutput(t *testing.T) {
	dists := []struct {
		Name     string   `json:"n"`
		Requires []string `json:"r"`
	}{
		{"requests", []string{"charset_normalizer (<4,>=2)", "idna (<4,>=2.5)", "urllib3 (<3,>=1.21.1)"}},
		{"charset-normalizer", nil},
		{"idna", nil},
		{"urllib3", nil},
		{"rich", []string{"markdown-it-py (>=2.2.0)", "pygments (>=2.13.0) ; extra == 'jupyter'"}},
	}

	display := make(map[string]string, len(dists))
	for _, d := range dists {
		if k := normalizeName(d.Name); k != "" {
			display[k] = d.Name
		}
	}

	deps := make(map[string][]string, len(dists))
	for _, d := range dists {
		key := display[normalizeName(d.Name)]
		var names []string
		for _, req := range d.Requires {
			if n, ok := display[reqName(req)]; ok {
				names = append(names, n)
			}
		}
		deps[key] = dedupeSorted(names)
	}
	dependents := invert(deps)

	want := []string{"charset-normalizer", "idna", "urllib3"}
	if got := deps["requests"]; !reflect.DeepEqual(got, want) {
		t.Errorf("requests depends on %v, want %v", got, want)
	}

	// The requirement is spelled charset_normalizer but must resolve to the
	// installed display name.
	if got := dependents["charset-normalizer"]; !reflect.DeepEqual(got, []string{"requests"}) {
		t.Errorf("charset-normalizer required by %v, want [requests]", got)
	}

	// rich's only requirement is an extra and must be dropped.
	if got := deps["rich"]; len(got) != 0 {
		t.Errorf("rich depends on %v, want none (extras are not installed)", got)
	}
}

func TestGetCreatedAt(t *testing.T) {
	env := t.TempDir()
	cfg := filepath.Join(env, "pyvenv.cfg")
	if err := os.WriteFile(cfg, []byte("version = 3.13.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 3, 13, 14, 13, 31, 0, time.UTC)
	if err := os.Chtimes(cfg, want, want); err != nil {
		t.Fatal(err)
	}

	if got := getCreatedAt(env); !got.Equal(want) {
		t.Errorf("getCreatedAt = %v, want %v", got, want)
	}
	if got := getCreatedAt(filepath.Join(env, "missing")); !got.IsZero() {
		t.Errorf("getCreatedAt on missing env = %v, want zero", got)
	}
}
