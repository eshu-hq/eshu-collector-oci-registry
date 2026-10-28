// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestCIWorkflowPinsActionsByCommitSHA keeps a moved tag from changing what
// pull-request CI executes. CI holds only contents: read, but it still runs
// third-party action code on every pull request.
func TestCIWorkflowPinsActionsByCommitSHA(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(".github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	var workflow workflowFile
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	if len(workflow.Jobs) == 0 {
		t.Fatal("ci.yml defines no jobs")
	}
	for name, job := range workflow.Jobs {
		if job.Uses != "" && !pinnedReusable.MatchString(job.Uses) {
			t.Errorf("job %s uses %q, want a full commit SHA pin", name, job.Uses)
		}
		for _, step := range job.Steps {
			if step.Uses != "" && !pinnedAction.MatchString(step.Uses) {
				t.Errorf("job %s uses %q, want a full commit SHA pin", name, step.Uses)
			}
		}
	}
}

// pinnedReusable keeps a moved tag from changing what pull-request CI
// executes: the reusable ref must be a full commit SHA (the `# v2` version
// comment is advisory text, not the pin).
var pinnedReusable = regexp.MustCompile(`^eshu-hq/\.github/\.github/workflows/go-collector-ci\.yml@[0-9a-f]{40}$`)

// wrapperCILanes are the only jobs the thin wrapper may define. Lane
// bodies live in the reusable, so local lint/test/race/shell/image/
// boundary/govulncheck/gosec/actionlint/ci-complete jobs must be gone:
// a leftover local lane would run beside (or shadow) the reusable one.
var wrapperCILanes = []string{"changes", "go-ci", "vuln-filing", "sec-filing"}

// TestCIWorkflowKeepsWrapperShape keeps the wrapper contract: only wrapper
// jobs exist, go-ci calls the reviewed reusable SHA, and no local lane job
// survived the move.
func TestCIWorkflowKeepsWrapperShape(t *testing.T) {
	t.Parallel()

	workflow := readCIWorkflow(t)
	for _, name := range wrapperCILanes {
		if _, ok := workflow.Jobs[name]; !ok {
			t.Errorf("ci.yml has no %q job, want the wrapper jobs %v", name, wrapperCILanes)
		}
	}
	for name := range workflow.Jobs {
		known := false
		for _, want := range wrapperCILanes {
			if name == want {
				known = true
			}
		}
		if !known {
			t.Errorf("ci.yml has unexpected job %q, want only the wrapper jobs %v", name, wrapperCILanes)
		}
	}
	call, ok := workflow.Jobs["go-ci"]
	if !ok {
		t.Fatal("ci.yml has no go-ci job")
	}
	if call.Uses != "eshu-hq/.github/.github/workflows/go-collector-ci.yml@4d01ab90baf5dd77c21cc1ee6bdf701332613449" {
		t.Errorf("go-ci uses %q, want the reviewed v2 SHA pin", call.Uses)
	}
	raw, err := os.ReadFile(".github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "# v2") {
		t.Errorf("ci.yml carries no # v2 version comment on the reusable pin")
	}
}

// TestCIWorkflowSetsJobTimeouts keeps a hung runner from holding a PR open
// forever: every lane declares a timeout.
func TestCIWorkflowSetsJobTimeouts(t *testing.T) {
	t.Parallel()

	workflow := readCIWorkflow(t)
	for name, job := range workflow.Jobs {
		if name == "go-ci" {
			// A `uses` job cannot carry timeout-minutes; the called
			// lanes carry their own (pinned upstream by selftest).
			continue
		}
		if job.Timeout <= 0 {
			t.Errorf("job %s sets no timeout-minutes", name)
		}
	}
}

// TestCIWorkflowCancelsSupersededRuns keeps stale pushes from consuming
// runners: one concurrency group per PR (or ref) with cancellation.
func TestCIWorkflowCancelsSupersededRuns(t *testing.T) {
	t.Parallel()

	workflow := readCIWorkflow(t)
	if group := fmt.Sprint(workflow.Concurrency["group"]); !strings.Contains(group, "github.event.pull_request.number") {
		t.Errorf("concurrency group = %q, want one group per PR", group)
	}
	if cancel := fmt.Sprint(workflow.Concurrency["cancel-in-progress"]); cancel != "true" {
		t.Errorf("cancel-in-progress = %q, want true", cancel)
	}
}

// TestCIWorkflowPRLeanesNeedNoConfiguredSecrets keeps fork PRs working: the
// only secret any lane may touch is the automatic GITHUB_TOKEN (used solely
// by the schedule-only issue step), and the workflow holds only
// contents: read.
func TestCIWorkflowPRLeanesNeedNoConfiguredSecrets(t *testing.T) {
	t.Parallel()

	workflow := readCIWorkflow(t)
	if got := workflow.Permissions["contents"]; got != "read" {
		t.Errorf("top-level contents permission = %q, want read", got)
	}
	for name, job := range workflow.Jobs {
		var texts []string
		for key, value := range job.Env {
			texts = append(texts, key+"="+value)
		}
		// Job-level `with:` inputs are a secret-passing surface since
		// the go-ci move: a future `token: ${{ secrets.X }}` must fail.
		for key, value := range job.With {
			texts = append(texts, key+"="+fmt.Sprint(value))
		}
		for _, step := range job.Steps {
			texts = append(texts, step.Run)
			for key, value := range step.Env {
				texts = append(texts, key+"="+value)
			}
			for key, value := range step.With {
				texts = append(texts, key+"="+fmt.Sprint(value))
			}
		}
		for _, text := range texts {
			for _, match := range secretUsePattern.FindAllStringSubmatch(text, -1) {
				if match[1] != "GITHUB_TOKEN" {
					t.Errorf("job %s uses configured secret %q, want no secrets but GITHUB_TOKEN", name, match[1])
				}
			}
		}
	}
}

// TestChangesDefinesFixtureAndMiscClasses keeps the skip matrix honest: the
// changes job must define the fixtures class (fixture-only PRs run test
// only) and the misc catch-all (unclassified files run the full Go lanes),
// with the some-with-excludes quantifier the misc negations need.
func TestChangesDefinesFixtureAndMiscClasses(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(".github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{"fixtures:", "misc:", "predicate-quantifier: 'some-with-excludes'"} {
		if !strings.Contains(text, want) {
			t.Errorf("ci.yml changes job lacks %q", want)
		}
	}
	// Every fixtures entry must be negated in misc, or a fixture-only PR
	// also trips misc and pays for lint and race.
	miscIdx := strings.Index(text, "misc:")
	if miscIdx < 0 {
		t.Fatal("ci.yml has no misc class")
	}
	misc := text[miscIdx:]
	for _, entry := range []string{"manifest.yaml", "grant.example.json", "config.example.yaml", "**/*.json", "**/*.sh", "Dockerfile"} {
		if !strings.Contains(misc, "'!"+entry+"'") {
			t.Errorf("misc class does not negate %q: a narrow-lane PR would also run the full lanes", entry)
		}
	}
}

// expectedRunInputs pins each run-* expression verbatim: the non-PR
// short-circuit is load-bearing (changes outputs are unused on push), and
// a widened clause silently pays for lanes the change class never needs.
// A dropped prefix or an extra clause fails this gate; rewire
// deliberately by updating both sides together.
var expectedRunInputs = map[string]string{
	"run-lint":        "${{ github.event_name != 'pull_request' || needs.changes.outputs.go == 'true' || needs.changes.outputs.misc == 'true' || needs.changes.outputs.workflows == 'true' }}",
	"run-test":        "${{ github.event_name != 'pull_request' || needs.changes.outputs.go == 'true' || needs.changes.outputs.fixtures == 'true' || needs.changes.outputs.shell == 'true' || needs.changes.outputs.misc == 'true' || needs.changes.outputs.workflows == 'true' }}",
	"run-race":        "${{ github.event_name != 'pull_request' || needs.changes.outputs.go == 'true' || needs.changes.outputs.misc == 'true' || needs.changes.outputs.workflows == 'true' }}",
	"run-shell":       "${{ github.event_name != 'pull_request' || needs.changes.outputs.shell == 'true' }}",
	"run-image":       "${{ github.event_name != 'pull_request' || needs.changes.outputs.image == 'true' || needs.changes.outputs.go == 'true' || needs.changes.outputs.workflows == 'true' }}",
	"run-boundary":    "${{ github.event_name != 'pull_request' || needs.changes.outputs.go == 'true' || needs.changes.outputs.misc == 'true' || needs.changes.outputs.workflows == 'true' }}",
	"run-govulncheck": "${{ github.event_name != 'pull_request' || needs.changes.outputs.deps == 'true' || needs.changes.outputs.workflows == 'true' }}",
	"run-gosec":       "${{ github.event_name != 'pull_request' || needs.changes.outputs.go == 'true' || needs.changes.outputs.misc == 'true' || needs.changes.outputs.workflows == 'true' }}",
	"run-actionlint":  "${{ github.event_name != 'pull_request' || needs.changes.outputs.workflows == 'true' }}",
}

// runInputProblems returns every way the go-ci wiring breaks the skip
// matrix the paths-filter promises: each run-* input must equal its
// pinned expression, or a narrow-lane PR silently pays for (or skips)
// the wrong lanes.
func runInputProblems(with map[string]any) []string {
	var problems []string
	get := func(key string) string { return fmt.Sprint(with[key]) }
	for key, want := range expectedRunInputs {
		if got := get(key); got != want {
			problems = append(problems, "go-ci "+key+" = "+got+", want "+want)
		}
	}
	if got := get("gosec-fail-on-findings"); got != "${{ github.event_name == 'schedule' }}" {
		problems = append(problems, "gosec-fail-on-findings = "+got+", want the schedule-only expression (filing unreachable otherwise)")
	}
	if got := get("coverage-floor"); got != "90.5" {
		problems = append(problems, "coverage-floor = "+got+", want the 90.5 ratchet (may only rise, deliberately)")
	}
	if got := get("gosec-version"); got != "v2.30.0" {
		problems = append(problems, "gosec-version = "+got+", want the triaged v2.30.0")
	}
	if cmd := get("boundary-command"); !strings.Contains(cmd, "TestImportBoundaryRejectsCoreAndCrossCollectorImports") {
		problems = append(problems, "boundary-command runs no import-boundary gate")
	}
	if cmd := get("image-command"); !strings.Contains(cmd, "seed") || !strings.Contains(cmd, "sha256") || !strings.Contains(cmd, "FROM") {
		problems = append(problems, "image-command skips the seed contract or digest pins")
	}
	return problems
}

// TestReusableWiringKeepsSkipMatrix pins the wrapper half of the skip
// matrix: the live text must pass, and a wiring with a dropped clause must
// fail (mirrors the old lane-level guards).
func TestReusableWiringKeepsSkipMatrix(t *testing.T) {
	t.Parallel()

	workflow := readCIWorkflow(t)
	call, ok := workflow.Jobs["go-ci"]
	if !ok {
		t.Fatal("ci.yml has no go-ci job")
	}
	if problems := runInputProblems(call.With); len(problems) > 0 {
		t.Errorf("go-ci wiring breaks the skip matrix:\n%s", strings.Join(problems, "\n"))
	}

	mutant := make(map[string]any, len(call.With))
	for key, value := range call.With {
		mutant[key] = value
	}
	mutant["run-image"] = strings.ReplaceAll(fmt.Sprint(call.With["run-image"]), "needs.changes.outputs.go == 'true' || ", "")
	if len(runInputProblems(mutant)) == 0 {
		t.Errorf("dropped image go clause: the gate accepted wiring that skips Go-source PRs")
	}
	prefix := make(map[string]any, len(call.With))
	for key, value := range call.With {
		prefix[key] = value
	}
	prefix["run-test"] = strings.ReplaceAll(fmt.Sprint(call.With["run-test"]), "github.event_name != 'pull_request' || ", "")
	if len(runInputProblems(prefix)) == 0 {
		t.Errorf("dropped non-PR prefix: the gate accepted wiring that skips push runs")
	}
	wide := make(map[string]any, len(call.With))
	for key, value := range call.With {
		wide[key] = value
	}
	wide["run-shell"] = strings.ReplaceAll(fmt.Sprint(call.With["run-shell"]), "needs.changes.outputs.shell", "needs.changes.outputs.shell || needs.changes.outputs.go")
	if len(runInputProblems(wide)) == 0 {
		t.Errorf("widened shell clause: the gate accepted wiring that pays for unneeded lanes")
	}
	floor := make(map[string]any, len(call.With))
	for key, value := range call.With {
		floor[key] = value
	}
	floor["coverage-floor"] = 74.1
	if len(runInputProblems(floor)) == 0 {
		t.Errorf("lowered floor: the gate accepted a coverage-floor drop")
	}
}

// TestFilingJobsNotifyOnSchedule keeps the schedule-only notification
// contract after the move: the scan steps live upstream, so each local
// filing job must exist with failure() plus schedule gates AND a gate on
// its own lane conclusion, or a lint/test/image/race failure on schedule
// files a mislabeled security-findings issue.
func TestFilingJobsNotifyOnSchedule(t *testing.T) {
	t.Parallel()

	workflow := readCIWorkflow(t)
	lanes := map[string]string{
		"vuln-filing": "check_name=go-ci%20%2F%20govulncheck",
		"sec-filing":  "check_name=go-ci%20%2F%20gosec",
	}
	for name, lane := range lanes {
		job, ok := workflow.Jobs[name]
		if !ok {
			t.Fatalf("ci.yml has no %q job", name)
		}
		if !strings.Contains(job.If, "failure()") || !strings.Contains(job.If, "schedule") {
			t.Errorf("job %s if = %q, want failure() plus schedule", name, job.If)
		}
		gated, files, loud := false, false, false
		for _, step := range job.Steps {
			if strings.Contains(step.Run, lane) && strings.Contains(step.Run, `!= "failure"`) {
				gated = true
			}
			if strings.Contains(step.Run, `"null"`) {
				loud = true
			}
			if strings.Contains(step.Run, "gh issue create") {
				files = true
			}
		}
		if !gated {
			t.Errorf("job %s files without gating on its lane conclusion (%s)", name, lane)
		}
		if !loud {
			t.Errorf("job %s skips silent on an empty lane conclusion", name)
		}
		if !files {
			t.Errorf("job %s files no issue", name)
		}
	}
}

func readCIWorkflow(t *testing.T) workflowFile {
	t.Helper()

	raw, err := os.ReadFile(".github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	var workflow workflowFile
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	if len(workflow.Jobs) == 0 {
		t.Fatal("ci.yml defines no jobs")
	}
	return workflow
}
