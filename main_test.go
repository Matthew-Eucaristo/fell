package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Matthew-Eucaristo/fell/internal/contract"
	tr "github.com/Matthew-Eucaristo/fell/internal/testrepo"
)

// capture runs fell with args and returns stdout and the exit code.
func capture(t *testing.T, stdin string, args ...string) (string, int) {
	t.Helper()
	r, w, _ := os.Pipe()
	oldOut, oldIn := os.Stdout, os.Stdin
	os.Stdout = w
	if stdin != "" {
		ir, iw, _ := os.Pipe()
		go func() { iw.WriteString(stdin); iw.Close() }()
		os.Stdin = ir
	}
	err := run(args)
	w.Close()
	os.Stdout, os.Stdin = oldOut, oldIn
	out, _ := io.ReadAll(r)
	code := 0
	if ee, ok := err.(*exitError); ok {
		code = ee.code
	} else if err != nil {
		t.Fatalf("fell %v: %v", args, err)
	}
	return string(out), code
}

func setup(t *testing.T) (root, wt string) {
	home := tr.Dir(t)
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	root = filepath.Join(home, "dev")
	repo := tr.Repo(t, root, "app")
	wt = tr.Worktree(t, repo, filepath.Join(repo, ".claude", "worktrees", "agent-a0b1c2d3"), "--detach")
	tr.NodeModules(t, wt)
	return root, wt
}

func TestScanJSONIsHomeRelative(t *testing.T) {
	root, _ := setup(t)
	out, code := capture(t, "", "scan", "--json", "--no-agents", root)
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	var doc contract.ScanDoc
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.SchemaVersion != 1 || len(doc.Items) != 1 {
		t.Fatalf("doc = %+v", doc)
	}
	it := doc.Items[0]
	if !strings.HasPrefix(it.Path, "~/") || it.Owner.ID != "claude-code" || it.Strippable.Allocated <= 0 {
		t.Errorf("item = %+v", it)
	}
	if home, _ := os.UserHomeDir(); strings.Contains(out, home) {
		t.Error("JSON contains the absolute home path")
	}
}

func TestNDJSONStreams(t *testing.T) {
	root, _ := setup(t)
	out, _ := capture(t, "", "scan", "--ndjson", "--no-agents", root)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[0], `{"type":"header"`) || !strings.HasPrefix(lines[1], `{"type":"item"`) || !strings.HasPrefix(lines[2], `{"type":"summary"`) {
		t.Fatalf("ndjson:\n%s", out)
	}
}

func TestApplyPlanDryRunAndStale(t *testing.T) {
	root, wt := setup(t)
	out, _ := capture(t, "", "scan", "--json", "--no-agents", root)
	var doc contract.ScanDoc
	json.Unmarshal([]byte(out), &doc)
	it := doc.Items[0]
	plan := `{"schema_version":1,"items":[{"id":"` + it.ID + `","fingerprint":"` + it.Fingerprint + `","action":"strip"}]}`
	res, code := capture(t, plan, "apply", "--plan", "-", "--dry-run", "--json", "--no-agents", root)
	if code != 0 || !strings.Contains(res, `"status": "dry_run"`) {
		t.Fatalf("dry-run apply exit %d:\n%s", code, res)
	}
	if _, err := os.Stat(filepath.Join(wt, "node_modules")); err != nil {
		t.Fatal("dry run removed node_modules")
	}
	stale := strings.Replace(plan, it.Fingerprint, "fp-0000000000000000", 1)
	res, code = capture(t, stale, "apply", "--plan", "-", "--json", "--no-agents", root)
	if code != contract.ExitRefused || !strings.Contains(res, "fingerprint") {
		t.Fatalf("stale plan: exit %d\n%s", code, res)
	}
	if _, err := os.Stat(filepath.Join(wt, "node_modules")); err != nil {
		t.Fatal("stale plan removed node_modules")
	}
}

func TestUsageErrors(t *testing.T) {
	for _, args := range [][]string{{"strip"}, {"apply"}, {"retire", "--permanent", "x"}, {"--bogus"}} {
		if _, code := capture(t, "", args...); code != contract.ExitUsage {
			t.Errorf("fell %v: exit %d, want %d", args, code, contract.ExitUsage)
		}
	}
}

func TestCapabilities(t *testing.T) {
	out, code := capture(t, "", "capabilities", "--json")
	var c contract.Capabilities
	if code != 0 || json.Unmarshal([]byte(out), &c) != nil || c.SchemaVersion != 1 || len(c.Actions) != 2 {
		t.Fatalf("capabilities: %d %s", code, out)
	}
}
