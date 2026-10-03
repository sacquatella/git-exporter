package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigurationMerge(t *testing.T) {
	dir := t.TempDir()
	filename := filepath.Join(dir, "config.yaml")
	data := "output: out\nprojects:\n  - name: one\n    source: ../local\n    ref: main\n    ssh_key: keys/id\n  - name: two\n    source: https://github.com/org/repo.git\n"
	if err := os.WriteFile(filename, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(filename, Overrides{Repos: []string{"three=./third"}, Fields: map[string][]string{"ref": {"one=release"}}})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Output != filepath.Join(dir, "out") || cfg.Projects[0].Source != filepath.Clean(filepath.Join(dir, "../local")) || cfg.Projects[0].SSHKey != filepath.Join(dir, "keys/id") || cfg.Projects[0].Ref != "release" || cfg.Projects[2].Source != "./third" {
		t.Fatalf("unexpected configuration: %+v", cfg)
	}
	cfg, err = Load(filename, Overrides{Output: "cli-output", OutputSet: true, Repos: []string{"one=./override"}})
	if err != nil || cfg.Output != "cli-output" || cfg.Projects[0].Source != "./override" {
		t.Fatalf("%+v %v", cfg, err)
	}
}

func TestInvalidConfiguration(t *testing.T) {
	for _, test := range []struct {
		name, yaml string
		overrides  Overrides
	}{
		{name: "unknown project", overrides: Overrides{Output: "out", OutputSet: true, Repos: []string{"one=local"}, Fields: map[string][]string{"ref": {"missing=main"}}}},
		{name: "unsafe name", overrides: Overrides{Output: "out", OutputSet: true, Repos: []string{"../escape=local"}}},
		{name: "duplicate CLI", overrides: Overrides{Output: "out", OutputSet: true, Repos: []string{"one=local", "one=other"}}},
		{name: "duplicate YAML", yaml: "output: out\nprojects:\n- name: p\n  source: a\n- name: p\n  source: b\n"},
		{name: "unknown field", yaml: "output: out\nprojectz: []\n"},
		{name: "embedded password", overrides: Overrides{Output: "out", OutputSet: true, Repos: []string{"one=https://user:password@example.test/repo"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			filename := ""
			if test.yaml != "" {
				filename = filepath.Join(t.TempDir(), "config.yaml")
				os.WriteFile(filename, []byte(test.yaml), 0600)
			}
			if _, err := Load(filename, test.overrides); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
