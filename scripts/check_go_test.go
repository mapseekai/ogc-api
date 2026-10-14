package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckLayout(t *testing.T) {
	for _, test := range []struct {
		name      string
		files     map[string]string
		wantError bool
	}{
		{"matching external test", map[string]string{
			"sample.go":      "package sample\nfunc Read() {}\n",
			"sample_test.go": "package sample_test\nimport \"testing\"\nconst size = 1\nfunc TestRead(t *testing.T) {}\nfunc helper(t *testing.T) { t.Helper() }\n",
		}, false},
		{"matching internal test", map[string]string{
			"sample.go":      "package sample\nfunc read() {}\n",
			"sample_test.go": "package sample\nimport \"testing\"\nfunc TestRead(t *testing.T) {}\n",
		}, false},
		{"unmatched test", map[string]string{
			"sample.go":                 "package sample\n",
			"sample_regression_test.go": "package sample\nimport \"testing\"\nfunc TestRead(t *testing.T) {}\n",
		}, true},
		{"test after helper type", map[string]string{
			"sample.go":      "package sample\n",
			"sample_test.go": "package sample\nimport \"testing\"\ntype fixture struct{}\nfunc TestRead(t *testing.T) {}\n",
		}, true},
		{"late constant", map[string]string{
			"sample.go": "package sample\nfunc Read() {}\nconst size = 1\n",
		}, true},
		{"missing helper marker", map[string]string{
			"sample.go":      "package sample\n",
			"sample_test.go": "package sample\nimport \"testing\"\nfunc TestRead(t *testing.T) {}\nfunc helper(t *testing.T) {}\n",
		}, true},
		{"late helper marker", map[string]string{
			"sample.go":      "package sample\n",
			"sample_test.go": "package sample\nimport \"testing\"\nfunc helper(t *testing.T) { t.Log(1); t.Helper() }\n",
		}, true},
		{"consistent receivers", map[string]string{
			"sample.go": "package sample\ntype Reader struct{}\nfunc (r *Reader) Read() {}\nfunc (r *Reader) Close() {}\n",
		}, false},
		{"inconsistent receivers across files", map[string]string{
			"sample.go": "package sample\ntype Reader struct{}\nfunc (r *Reader) Read() {}\n",
			"other.go":  "package sample\nfunc (reader *Reader) Close() {}\n",
		}, true},
		{"nonstandard file name", map[string]string{
			"sampleReader.go": "package sample\n",
		}, true},
		{"artifact exclusion", map[string]string{
			"sample.go":                    "package sample\n",
			".artifacts/unmatched_test.go": "not Go source",
		}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			for name, content := range test.files {
				writeSource(t, root, name, content)
			}
			if err := checkLayout(root); (err != nil) != test.wantError {
				t.Fatalf("checkLayout() = %v, want error=%t", err, test.wantError)
			}
		})
	}
}

func writeSource(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
