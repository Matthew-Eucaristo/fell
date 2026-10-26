package contract

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Matthew-Eucaristo/fell/internal/clean"
)

func TestReadPlanValidates(t *testing.T) {
	good := `{"schema_version":1,"items":[{"id":"wt-1","fingerprint":"fp-1","action":"strip","artifacts":["art-1"]}]}`
	if _, err := ReadPlan(strings.NewReader(good)); err != nil {
		t.Fatal(err)
	}
	bad := map[string]string{
		"version":     `{"schema_version":2,"items":[{"id":"a","fingerprint":"b","action":"strip"}]}`,
		"empty":       `{"schema_version":1,"items":[]}`,
		"no fp":       `{"schema_version":1,"items":[{"id":"a","action":"strip"}]}`,
		"action":      `{"schema_version":1,"items":[{"id":"a","fingerprint":"b","action":"delete"}]}`,
		"unknown key": `{"schema_version":1,"force":true,"items":[{"id":"a","fingerprint":"b","action":"strip"}]}`,
		"artifacts":   `{"schema_version":1,"items":[{"id":"a","fingerprint":"b","action":"retire","artifacts":["x"]}]}`,
	}
	for name, p := range bad {
		if _, err := ReadPlan(strings.NewReader(p)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestLineAddsType(t *testing.T) {
	var b bytes.Buffer
	Line(&b, "item", map[string]int{"a": 1})
	Line(&b, "summary", struct{}{})
	lines := strings.Split(strings.TrimSpace(b.String()), "\n")
	var v map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &v); err != nil || v["type"] != "item" || v["a"] != float64(1) {
		t.Errorf("line 0 = %s", lines[0])
	}
	if lines[1] != `{"type":"summary"}` {
		t.Errorf("line 1 = %s", lines[1])
	}
}

func TestTildeRoundTrip(t *testing.T) {
	t.Setenv("HOME", "/home/someone")
	if got := Tilde("/home/someone/dev/x"); got != "~/dev/x" {
		t.Errorf("Tilde = %s", got)
	}
	if got := Tilde("/home/someoneelse/x"); got != "/home/someoneelse/x" {
		t.Errorf("Tilde touched a sibling: %s", got)
	}
	if got := Expand("~/dev/x"); got != "/home/someone/dev/x" {
		t.Errorf("Expand = %s", got)
	}
}

func TestExitCodes(t *testing.T) {
	tool := NewTool("test")
	mk := func(st ...string) ApplyDoc {
		var outs []*clean.Outcome
		for _, s := range st {
			outs = append(outs, &clean.Outcome{Status: s})
		}
		return NewApplyDoc(tool, clean.Trash, outs)
	}
	if c := mk(clean.StatusDone).ExitCode(); c != ExitOK {
		t.Errorf("done -> %d", c)
	}
	if c := mk(clean.StatusDone, clean.StatusRefused).ExitCode(); c != ExitRefused {
		t.Errorf("refused -> %d", c)
	}
	if c := mk(clean.StatusRefused, clean.StatusFailed).ExitCode(); c != ExitPartial {
		t.Errorf("failed -> %d", c)
	}
}

func TestCapabilitiesListEveryReasonCode(t *testing.T) {
	c := NewCapabilities(NewTool("test"))
	b, _ := json.Marshal(c)
	for _, code := range []string{"live", "in_use", "dirty", "only_here", "app_tracked", "keep_marker", "locked", "unknown_state", "nested_worktree", "unpushed", "no_remote", "ignored_precious", "ignored_data", "liveness_unknown"} {
		if !strings.Contains(string(b), `"`+code+`"`) {
			t.Errorf("capabilities lack %s", code)
		}
	}
}
