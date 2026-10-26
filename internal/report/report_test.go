package report

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Matthew-Eucaristo/fell/internal/scan"
)

func TestTextHeaderNamesScope(t *testing.T) {
	for _, agents := range []bool{true, false} {
		var b bytes.Buffer
		Text(&b, &scan.Result{}, []string{"/work"}, agents)
		head, _, _ := strings.Cut(b.String(), "\n")
		if got := strings.Contains(head, "agent locations"); got != agents {
			t.Errorf("agents=%v: header %q", agents, head)
		}
	}
}
