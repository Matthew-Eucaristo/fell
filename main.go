// Command fell finds git worktrees left behind by AI coding agents and
// people, shows the evidence for each, and strips or retires them
// reversibly. Run `fell -h` for usage.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Matthew-Eucaristo/fell/internal/agents"
	"github.com/Matthew-Eucaristo/fell/internal/clean"
	"github.com/Matthew-Eucaristo/fell/internal/contract"
	"github.com/Matthew-Eucaristo/fell/internal/report"
	"github.com/Matthew-Eucaristo/fell/internal/scan"
	"github.com/Matthew-Eucaristo/fell/internal/ui"
)

var version = "" // set by -ldflags at release time

const usage = `fell - find and safely clean up git worktrees, especially ones AI agents left behind.

Usage:
  fell [flags] [root...]                  interactive; roots default to ~/dev
  fell scan [--json|--ndjson] [root...]   list worktrees with evidence (read-only)
  fell strip  [flags] <id|path>...        move rebuildable artifacts to the Trash, keep the worktree
  fell retire [flags] <id|path>...        save a rescue folder, trash the checkout, prune git's record
  fell apply --plan <file|-> [root...]    run a JSON plan of ids + fingerprints from a scan
  fell capabilities --json                describe actions, reason codes and exit codes

Nothing is deleted permanently: everything goes to the Trash, and retire
first saves commits that exist nowhere else, uncommitted changes and
git-ignored secrets. Worktrees an app still tracks are only stripped; fell
tells you how that app removes them.

Flags:
`

type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string {
	if e.err == nil {
		return ""
	}
	return e.err.Error()
}

func main() {
	err := run(os.Args[1:])
	var ee *exitError
	switch {
	case err == nil:
	case errors.As(err, &ee):
		if ee.err != nil {
			fmt.Fprintln(os.Stderr, "fell:", ee.err)
		}
		os.Exit(ee.code)
	case errors.Is(err, flag.ErrHelp):
	default:
		fmt.Fprintln(os.Stderr, "fell:", err)
		os.Exit(contract.ExitError)
	}
}

func run(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "scan":
			return runScan(args[1:])
		case "strip", "retire":
			return runAction(args[0], args[1:])
		case "apply":
			return runApply(args[1:])
		case "capabilities":
			return runCapabilities(args[1:])
		case "version":
			fmt.Println("fell", buildVersion())
			return nil
		}
	}
	return runDefault(args)
}

func usageErr(format string, a ...any) error {
	return &exitError{contract.ExitUsage, fmt.Errorf(format, a...)}
}

func newFlags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), usage)
		fs.PrintDefaults()
	}
	return fs
}

func parse(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return err
		}
		return &exitError{contract.ExitUsage, nil}
	}
	return nil
}

func runDefault(args []string) error {
	fs := newFlags("fell")
	var (
		dryRun   = fs.Bool("dry-run", false, "never change anything; show what actions would do")
		jsonOut  = fs.Bool("json", false, "print the scan as JSON and exit (same as `fell scan --json`)")
		list     = fs.Bool("list", false, "print the scan as a table and exit (default when not a terminal)")
		noAgents = fs.Bool("no-agents", false, "only scan the given roots, not known agent locations")
		showVer  = fs.Bool("version", false, "print version")
	)
	if err := parse(fs, args); err != nil {
		return err
	}
	if *showVer {
		fmt.Println("fell", buildVersion())
		return nil
	}
	opts, err := scanOptions(fs.Args(), !*noAgents)
	if err != nil {
		return err
	}
	interactive := isTerminal(os.Stdout) && isTerminal(os.Stdin)
	if *jsonOut || *list || !interactive {
		res, err := scan.Scan(opts)
		if err != nil {
			return err
		}
		res.MeasureAll()
		if *jsonOut {
			return contract.Encode(os.Stdout, contract.NewScanDoc(res, opts.Roots, contract.NewTool(buildVersion())))
		}
		report.Text(os.Stdout, res, opts.Roots, opts.Agents)
		return nil
	}
	mode := clean.Trash
	if *dryRun {
		mode = clean.DryRun
	}
	m := ui.New(opts, clean.Options{Mode: mode, Env: opts.Env})
	if _, err := tea.NewProgram(m).Run(); err != nil {
		return err
	}
	report.Outcomes(os.Stdout, m.Outcomes(), mode)
	return nil
}

func runScan(args []string) error {
	fs := newFlags("fell scan")
	var (
		jsonOut  = fs.Bool("json", false, "print one JSON document")
		ndjson   = fs.Bool("ndjson", false, "stream newline-delimited JSON: a header, one item per worktree as it is measured, a summary")
		noAgents = fs.Bool("no-agents", false, "only scan the given roots, not known agent locations")
	)
	if err := parse(fs, args); err != nil {
		return err
	}
	opts, err := scanOptions(fs.Args(), !*noAgents)
	if err != nil {
		return err
	}
	res, err := scan.Scan(opts)
	if err != nil {
		return err
	}
	tool := contract.NewTool(buildVersion())
	if *ndjson {
		return streamScan(os.Stdout, res, opts.Roots, tool)
	}
	res.MeasureAll()
	if *jsonOut {
		return contract.Encode(os.Stdout, contract.NewScanDoc(res, opts.Roots, tool))
	}
	report.Text(os.Stdout, res, opts.Roots, opts.Agents)
	return nil
}

func streamScan(w io.Writer, res *scan.Result, roots []string, tool contract.Tool) error {
	head := contract.NewScanDoc(&scan.Result{Warnings: res.Warnings}, roots, tool)
	head.Summary.Items = len(res.Worktrees)
	if err := contract.Line(w, "header", struct {
		SchemaVersion int      `json:"schema_version"`
		Tool          any      `json:"tool"`
		Roots         []string `json:"roots"`
		Warnings      []string `json:"warnings"`
		Items         int      `json:"items"`
	}{head.SchemaVersion, head.Tool, head.Roots, head.Warnings, len(res.Worktrees)}); err != nil {
		return err
	}
	byID := map[string]*scan.Worktree{}
	for _, wt := range res.Worktrees {
		byID[wt.ID] = wt
	}
	ch := make(chan scan.Measurement)
	go res.MeasureInto(ch)
	var items []contract.Item
	for m := range ch {
		wt := byID[m.ID]
		wt.Apply(m)
		it := contract.NewItem(wt, head.GeneratedAt)
		items = append(items, it)
		if err := contract.Line(w, "item", it); err != nil {
			return err
		}
	}
	return contract.Line(w, "summary", contract.Summarize(items))
}

func runCapabilities(args []string) error {
	fs := newFlags("fell capabilities")
	fs.Bool("json", true, "print JSON (the only format)")
	if err := parse(fs, args); err != nil {
		return err
	}
	return contract.Encode(os.Stdout, contract.NewCapabilities(contract.NewTool(buildVersion())))
}

// actionFlags are shared by strip, retire and apply.
type actionFlags struct {
	dryRun, force, unlock, permanent, jsonOut, noAgents *bool
	roots                                               *string
}

func addActionFlags(fs *flag.FlagSet, action string) actionFlags {
	f := actionFlags{
		dryRun:   fs.Bool("dry-run", false, "check everything and report, change nothing"),
		force:    fs.Bool("force", false, "override refusals that can be overridden: dirty, in use, commits only here, unknown state (never: live, app-tracked, keep marker)"),
		jsonOut:  fs.Bool("json", false, "print results as JSON"),
		noAgents: fs.Bool("no-agents", false, "only scan the given roots, not known agent locations"),
	}
	if action != scan.ActionStrip {
		f.unlock = fs.Bool("unlock", false, "allow `git worktree unlock` on locked worktrees whose owner is not running")
	} else {
		f.unlock = new(bool)
	}
	if action != scan.ActionRetire {
		f.permanent = fs.Bool("permanent", false, "strip only: delete artifacts instead of moving them to the Trash")
	} else {
		f.permanent = new(bool)
	}
	if action != "apply" {
		f.roots = fs.String("root", "", "comma-separated roots to scan when resolving ids (default ~/dev)")
	} else {
		f.roots = new(string)
	}
	return f
}

func (f actionFlags) cleanOptions(env agents.Env) (clean.Options, error) {
	o := clean.Options{Mode: clean.Trash, Force: *f.force, Unlock: *f.unlock, Env: env}
	switch {
	case *f.dryRun && *f.permanent:
		return o, usageErr("--dry-run and --permanent are mutually exclusive")
	case *f.dryRun:
		o.Mode = clean.DryRun
	case *f.permanent:
		o.Mode = clean.Permanent
	}
	return o, nil
}

func runAction(action string, args []string) error {
	fs := newFlags("fell " + action)
	f := addActionFlags(fs, action)
	if err := parse(fs, args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return usageErr("%s needs at least one worktree id or path", action)
	}
	var roots []string
	if *f.roots != "" {
		roots = strings.Split(*f.roots, ",")
	}
	// Paths given directly are scanned too, so they resolve without --root.
	for _, ref := range fs.Args() {
		if p := contract.Expand(ref); strings.ContainsRune(p, filepath.Separator) {
			if fi, err := os.Stat(filepath.Dir(p)); err == nil && fi.IsDir() {
				roots = append(roots, filepath.Dir(p))
			}
		}
	}
	opts, err := scanOptions(roots, !*f.noAgents)
	if err != nil {
		return err
	}
	co, err := f.cleanOptions(opts.Env)
	if err != nil {
		return err
	}
	res, err := scan.Scan(opts)
	if err != nil {
		return err
	}
	var outs []*clean.Outcome
	for _, ref := range fs.Args() {
		w := res.Find(ref)
		if w == nil {
			outs = append(outs, &clean.Outcome{ID: ref, Action: action, Path: ref, Status: clean.StatusRefused, Error: "no worktree with this id or path under the scanned roots", Freed: -1})
			continue
		}
		outs = append(outs, act(action, w, "", nil, co))
	}
	return finish(outs, co, *f.jsonOut)
}

func runApply(args []string) error {
	fs := newFlags("fell apply")
	f := addActionFlags(fs, "apply")
	planPath := fs.String("plan", "", "plan file, or - for stdin")
	if err := parse(fs, args); err != nil {
		return err
	}
	if *planPath == "" {
		return usageErr("apply needs --plan <file|->")
	}
	var in io.Reader = os.Stdin
	if *planPath != "-" {
		file, err := os.Open(*planPath)
		if err != nil {
			return err
		}
		defer file.Close()
		in = file
	}
	plan, err := contract.ReadPlan(in)
	if err != nil {
		return &exitError{contract.ExitUsage, err}
	}
	opts, err := scanOptions(fs.Args(), !*f.noAgents)
	if err != nil {
		return err
	}
	co, err := f.cleanOptions(opts.Env)
	if err != nil {
		return err
	}
	res, err := scan.Scan(opts)
	if err != nil {
		return err
	}
	var outs []*clean.Outcome
	for _, it := range plan.Items {
		w := res.Find(it.ID)
		if w == nil {
			outs = append(outs, &clean.Outcome{ID: it.ID, Action: it.Action, Status: clean.StatusRefused, Error: "not found under the scanned roots; rescan", Freed: -1})
			continue
		}
		var only []string
		for _, id := range it.Artifacts {
			for _, a := range w.Artifacts {
				if a.ID == id {
					only = append(only, a.Path)
				}
			}
		}
		if len(it.Artifacts) > 0 && len(only) != len(it.Artifacts) {
			outs = append(outs, &clean.Outcome{ID: it.ID, Action: it.Action, Path: w.Path, Status: clean.StatusRefused, Error: "unknown artifact id; rescan", Freed: -1})
			continue
		}
		outs = append(outs, act(it.Action, w, it.Fingerprint, only, co))
	}
	return finish(outs, co, *f.jsonOut)
}

func act(action string, w *scan.Worktree, fingerprint string, only []string, co clean.Options) *clean.Outcome {
	if action == scan.ActionStrip {
		return clean.Strip(w, fingerprint, only, co)
	}
	if co.Mode == clean.Permanent {
		return &clean.Outcome{ID: w.ID, Action: action, Path: w.Path, Status: clean.StatusRefused, Error: "retire never deletes permanently", Freed: -1}
	}
	return clean.Retire(w, fingerprint, co)
}

func finish(outs []*clean.Outcome, co clean.Options, jsonOut bool) error {
	doc := contract.NewApplyDoc(contract.NewTool(buildVersion()), co.Mode, outs)
	if jsonOut {
		if err := contract.Encode(os.Stdout, doc); err != nil {
			return err
		}
	} else {
		report.Outcomes(os.Stdout, outs, co.Mode)
	}
	if code := doc.ExitCode(); code != contract.ExitOK {
		return &exitError{code, nil}
	}
	return nil
}

// scanOptions resolves roots (default ~/dev, else the current directory).
func scanOptions(args []string, withAgents bool) (scan.Options, error) {
	env := agents.DefaultEnv()
	opts := scan.Options{Agents: withAgents, Env: env}
	if len(args) == 0 {
		def := filepath.Join(env.Home, "dev")
		if fi, err := os.Stat(def); err != nil || !fi.IsDir() {
			def = "."
		}
		args = []string{def}
	}
	seen := map[string]bool{}
	for _, a := range args {
		abs, err := filepath.Abs(contract.Expand(a))
		if err != nil {
			return opts, err
		}
		real, err := filepath.EvalSymlinks(abs)
		if err != nil {
			return opts, &exitError{contract.ExitUsage, err}
		}
		if fi, err := os.Stat(real); err != nil || !fi.IsDir() {
			return opts, usageErr("%s is not a directory", a)
		}
		if !seen[real] {
			seen[real] = true
			opts.Roots = append(opts.Roots, real)
		}
	}
	return opts, nil
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func buildVersion() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return strings.TrimPrefix(info.Main.Version, "v")
	}
	return "dev"
}
