package launch

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/board"
)

func fixtureGit(t *testing.T, cwd string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", cwd, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid"}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %s %v", args, out, err)
	}
	return strings.TrimSpace(string(out))
}

func fixtureCommit(t *testing.T, cwd, name, body, message string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(cwd, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	fixtureGit(t, cwd, "add", name)
	fixtureGit(t, cwd, "commit", "-m", message)
	return fixtureGit(t, cwd, "rev-parse", "HEAD")
}

// advancedWrapFixture closes a batch whose target moved through a fix round, so
// the plan's last recorded target is the closed final target wrap-up must bind.
func advancedWrapFixture(t *testing.T) (string, string, string, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake terminal observation; Git-only test runs natively")
	}
	root, _, _ := setupBoard(t)
	task, path := makeTodo(t, root, "final-target")
	startThenReview(t, root, "claude", task, path)
	cwd, base := integrationGit(t)
	final := fixtureCommit(t, cwd, "fix.txt", "fixed\n", "fix round")
	roles := map[string]string{"PM": "N/A: lifecycle fixture", "QA": "N/A: lifecycle fixture", "CSA": "N/A: fixture", "Hacker": "N/A: fixture"}
	plan := board.ReviewPlan{Schema: 1, Sealed: true, PlanID: "final-plan", Author: "fixture", Basis: "lifecycle fixture", CWD: cwd, ReportLanguage: "zh-CN", TaskIDs: []string{task}, Batches: []board.ReviewPlanBatch{{BatchID: "final-batch", TaskIDs: []string{task}, Base: base, TargetCommit: base, Requirements: roles}}}
	if err := board.CreateReviewPlan(root, plan); err != nil {
		t.Fatal(err)
	}
	b, err := board.ReadReviewBatch(root, "final-batch")
	if err != nil {
		t.Fatal(err)
	}
	if err = board.AdvanceReviewBatch(root, board.ReviewBatchAdvance{BatchID: "final-batch", ExpectedRevision: b.Revision, Advance: board.ReviewAdvance{PreviousTarget: base, Target: final, Reason: "同批修复交付", Deliveries: map[string]string{final: task}}}); err != nil {
		t.Fatal(err)
	}
	view, err := board.ReadReviewBatchView(root, "final-batch")
	if err != nil {
		t.Fatal(err)
	}
	request := board.ReviewCloseRequest{BatchID: "final-batch", ExpectedRevision: view.Batch.Revision, ViewHash: board.ReviewViewDigest(view), Author: "fixture", Roles: map[string]board.ReviewRoleConclusion{}}
	edges, _, err := board.ReviewClosureEdges(view, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = board.CloseReviewBatch(root, request, board.ReviewGitEvidence{CWD: cwd, Head: final, Edges: edges, VerifiedAt: time.Now().UTC().Format(time.RFC3339Nano)}); err != nil {
		t.Fatal(err)
	}
	return root, task, cwd, base
}

func TestWrapUpBindsClosedFinalTargetAfterAdvance(t *testing.T) {
	root, task, cwd, base := advancedWrapFixture(t)
	final := fixtureGit(t, cwd, "rev-parse", "HEAD")
	reviewRange, err := board.DispatchReviewRange(root, task)
	if err != nil {
		t.Fatal(err)
	}
	if reviewRange.Ancestor != base || reviewRange.Descendant != final {
		t.Fatalf("range %s..%s is not base..closed final target", reviewRange.Ancestor, reviewRange.Descendant)
	}
	bound := func(id, source string) board.DispatchInput {
		return board.DispatchInput{ID: id, TaskID: task, Kind: "wrap-up", Message: "只清理和记录", Base: source, Evidence: board.DispatchEvidence{WrapUp: &board.DispatchWrapUpBinding{Git: board.DispatchIntegration{CWD: cwd, SourceCommit: source, ReviewTarget: source, ReviewBase: base, TargetCommit: source, TargetRef: "refs/heads/develop", Author: "coordinator", Basis: "本地 develop 实际祖先验证"}}}}
	}
	if _, err = PrepareBoundDispatch(root, bound("wrap-final", final)); err != nil {
		t.Fatalf("closed final target rejected: %v", err)
	}
	_, err = PrepareBoundDispatch(root, bound("wrap-stale", base))
	if err == nil || !strings.Contains(err.Error(), "final review target") {
		t.Fatalf("stale registered target accepted: %v", err)
	}
}

func TestWrapUpRebaseComparesClosedFinalRange(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Git-only test runs natively")
	}
	cwd, base := integrationGit(t)
	final := fixtureCommit(t, cwd, "fix.txt", "fixed\n", "fix round")
	fixtureGit(t, cwd, "checkout", "-q", "-b", "rebased", base)
	rebasedBase := fixtureCommit(t, cwd, "unrelated.txt", "other\n", "unrelated advance")
	source := fixtureCommit(t, cwd, "fix.txt", "fixed\n", "fix round rebased")
	g := board.DispatchIntegration{CWD: cwd, ReviewBase: base, ReviewTarget: final, RebasedBase: rebasedBase, SourceCommit: source}
	if err := verifyDispatchRebase(context.Background(), g); err != nil {
		t.Fatalf("rebased delivery of the closed final target rejected: %v", err)
	}
	g.ReviewTarget = base
	if err := verifyDispatchRebase(context.Background(), g); err == nil {
		t.Fatal("comparison against the pre-advance target accepted")
	}
}
