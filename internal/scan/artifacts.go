package scan

import (
	"os"
	"path/filepath"
	"strings"
)

// ArtifactRule describes a rebuildable directory. A directory is an
// artifact candidate only when its name matches and one of Markers exists
// next to it (or Inside it, for virtualenvs). The cleaner additionally
// requires git to ignore it and to track nothing inside it.
type ArtifactRule struct {
	Name       string
	Markers    []string // sibling files, any of which must exist; empty = none needed
	Inside     string   // a file that must exist inside the directory instead
	Regenerate string   // how it comes back, for the UI and the contract
}

// ArtifactRules is the catalog. Keep it to directories that a package
// manager or build tool recreates from tracked files.
var ArtifactRules = []ArtifactRule{
	{Name: "node_modules", Markers: []string{"package.json"}, Regenerate: "npm / pnpm / yarn / bun install"},
	{Name: ".next", Markers: []string{"package.json"}, Regenerate: "next build / next dev"},
	{Name: ".nuxt", Markers: []string{"package.json"}, Regenerate: "nuxt build / nuxt dev"},
	{Name: ".svelte-kit", Markers: []string{"package.json"}, Regenerate: "vite build / vite dev"},
	{Name: ".turbo", Markers: []string{"package.json", "turbo.json"}, Regenerate: "the next turbo run"},
	{Name: ".parcel-cache", Markers: []string{"package.json"}, Regenerate: "the next parcel build"},
	{Name: ".angular", Markers: []string{"angular.json"}, Regenerate: "the next ng build"},
	{Name: ".expo", Markers: []string{"package.json"}, Regenerate: "the next expo start"},
	{Name: "dist", Markers: []string{"package.json", "pyproject.toml", "setup.py"}, Regenerate: "the project's build script"},
	{Name: "build", Markers: []string{"package.json", "pubspec.yaml", "build.gradle", "build.gradle.kts", "CMakeLists.txt", "pyproject.toml", "setup.py"}, Regenerate: "the project's build"},
	{Name: ".venv", Inside: "pyvenv.cfg", Regenerate: "uv sync / python -m venv + pip install"},
	{Name: "venv", Inside: "pyvenv.cfg", Regenerate: "python -m venv + pip install"},
	{Name: ".tox", Markers: []string{"tox.ini", "pyproject.toml", "setup.cfg"}, Regenerate: "tox"},
	{Name: ".pytest_cache", Regenerate: "pytest"},
	{Name: ".mypy_cache", Regenerate: "mypy"},
	{Name: ".ruff_cache", Regenerate: "ruff"},
	{Name: "target", Markers: []string{"Cargo.toml", "pom.xml"}, Regenerate: "cargo build / mvn package"},
	{Name: ".dart_tool", Markers: []string{"pubspec.yaml"}, Regenerate: "flutter pub get / dart pub get"},
	{Name: ".gradle", Markers: []string{"build.gradle", "build.gradle.kts", "settings.gradle", "settings.gradle.kts"}, Regenerate: "the next Gradle build"},
	{Name: ".cxx", Markers: []string{"build.gradle", "build.gradle.kts"}, Regenerate: "the next Android native build"},
	{Name: "Pods", Markers: []string{"Podfile"}, Regenerate: "pod install"},
	{Name: ".build", Markers: []string{"Package.swift"}, Regenerate: "swift build"},
	{Name: "vendor", Markers: []string{"composer.json"}, Regenerate: "composer install"},
	{Name: "_build", Markers: []string{"mix.exs"}, Regenerate: "mix compile"},
	{Name: "deps", Markers: []string{"mix.exs"}, Regenerate: "mix deps.get"},
	{Name: ".zig-cache", Markers: []string{"build.zig"}, Regenerate: "zig build"},
	{Name: "zig-out", Markers: []string{"build.zig"}, Regenerate: "zig build"},
	{Name: ".stack-work", Markers: []string{"stack.yaml"}, Regenerate: "stack build"},
}

var rulesByName = func() map[string]*ArtifactRule {
	m := map[string]*ArtifactRule{}
	for i := range ArtifactRules {
		m[ArtifactRules[i].Name] = &ArtifactRules[i]
	}
	return m
}()

// MatchArtifact returns the rule for dir if its name and markers match. It
// does not follow symlinks: dir itself must be a real directory.
func MatchArtifact(dir string) *ArtifactRule {
	r := rulesByName[filepath.Base(dir)]
	if r == nil {
		return nil
	}
	if fi, err := os.Lstat(dir); err != nil || !fi.IsDir() {
		return nil
	}
	if r.Inside != "" {
		if !isFile(filepath.Join(dir, r.Inside)) {
			return nil
		}
		return r
	}
	if len(r.Markers) == 0 {
		return r
	}
	parent := filepath.Dir(dir)
	for _, m := range r.Markers {
		if isFile(filepath.Join(parent, m)) {
			return r
		}
	}
	return nil
}

func isFile(p string) bool {
	fi, err := os.Lstat(p)
	return err == nil && fi.Mode().IsRegular()
}

// preciousNames are git-ignored files that are usually irreplaceable:
// secrets and local databases. Matching is on the base name.
func isPrecious(name string) bool {
	lower := strings.ToLower(name)
	switch {
	case lower == ".env" || lower == ".envrc" || lower == ".dev.vars":
		return true
	case strings.HasPrefix(lower, ".env."):
		return true
	case strings.HasSuffix(lower, ".local.json"):
		return true
	}
	switch filepath.Ext(lower) {
	case ".pem", ".key", ".p12", ".keystore", ".jks", ".sqlite", ".sqlite3", ".db":
		return true
	}
	return false
}
