package board

import (
	"strings"
	"testing"
)

func readPlanRecord(t *testing.T, root, planID string) ReviewPlan {
	t.Helper()
	p, err := ReadReviewPlan(root, planID)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func advanceGateBatch(t *testing.T, root, id, target string) {
	t.Helper()
	b, err := ReadReviewBatch(root, "batch")
	if err != nil {
		t.Fatal(err)
	}
	if err = AdvanceReviewBatch(root, ReviewBatchAdvance{BatchID: "batch", ExpectedRevision: b.Revision, Advance: ReviewAdvance{PreviousTarget: b.TargetCommit, Target: target, Reason: "同批修复交付", Deliveries: map[string]string{target: id}}}); err != nil {
		t.Fatal(err)
	}
}

// staleGateBatchTarget reproduces a board published before advance synced the
// plan: the runtime batch moved on while the plan still records the old target.
func staleGateBatchTarget(t *testing.T, root, target string) {
	t.Helper()
	err := WithTransaction(root, reviewScope(nil, false), func(tx *Transaction) error {
		var b ReviewBatch
		ok, e := readReviewJSON(tx, reviewBatchName("batch"), &b)
		if e != nil {
			return e
		}
		if !ok {
			return reviewError("missing batch")
		}
		b.TargetCommit = target
		b.Revision++
		return tx.PutGroup(reviewControlGroup, reviewBatchName("batch"), reviewJSON(b))
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAdvanceSyncsPlanTargetAndClosedRange(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "plan-target")
	p := readPlanRecord(t, root, gatePlan(t, root, []string{id}, noReviewRequirements()).PlanID)
	fix := strings.Repeat("c", 40)
	advanceGateBatch(t, root, id, fix)

	synced := readPlanRecord(t, root, p.PlanID)
	if last := synced.Batches[len(synced.Batches)-1]; last.TargetCommit != fix {
		t.Fatalf("plan target not synced: %s", last.TargetCommit)
	}
	if synced.Revision != p.Revision+1 {
		t.Fatalf("plan revision %d", synced.Revision)
	}
	if _, err := ReviewTaskProgress(root, id); err != nil {
		t.Fatalf("progress after sync: %v", err)
	}
	if problems := CheckReviewGate(root, []string{id}); len(problems) > 0 {
		t.Fatalf("check after sync: %+v", problems)
	}
	if _, err := gateClose(t, root, map[string]ReviewRoleConclusion{}); err != nil {
		t.Fatal(err)
	}
	edge, err := DispatchReviewRange(root, id)
	if err != nil {
		t.Fatal(err)
	}
	if edge.Descendant != fix {
		t.Fatalf("wrap-up range ends at %s, not the closed final target", edge.Descendant)
	}
}

func TestAdvanceFileRunSyncsOnlyPlannedBatches(t *testing.T) {
	t.Run("planned", func(t *testing.T) {
		root := tempBoard(t)
		id := gateCard(t, root, "advance-file")
		p := gatePlan(t, root, []string{id}, archiveRequirements())
		fix := strings.Repeat("c", 40)
		input := archiveInput([]string{id}, "pm-run", "PM")
		input.Commit = fix
		advance := &ReviewAdvance{PreviousTarget: strings.Repeat("b", 40), Target: fix, Reason: "修复后重跑", Deliveries: map[string]string{fix: id}}
		if _, _, err := PrepareReviewRun(root, input, nil, advance, archiveOriginals(), "test"); err != nil {
			t.Fatal(err)
		}
		if last := readPlanRecord(t, root, p.PlanID).Batches[0]; last.TargetCommit != fix {
			t.Fatalf("plan target not synced: %s", last.TargetCommit)
		}
		if _, err := ReviewTaskProgress(root, id); err != nil {
			t.Fatalf("progress after sync: %v", err)
		}
	})
	t.Run("unplanned", func(t *testing.T) {
		root := tempBoard(t)
		id := archiveCard(t, root, "advance-file-unplanned")
		fix := strings.Repeat("c", 40)
		first := archiveInput([]string{id}, "pm-run", "PM")
		publishRun(t, root, finalizedRun(t, root, first).RunID)
		second := archiveInput([]string{id}, "qa-run", "QA")
		second.Commit = fix
		advance := &ReviewAdvance{PreviousTarget: first.Commit, Target: fix, Reason: "修复后重跑", Deliveries: map[string]string{fix: id}}
		if _, _, err := PrepareReviewRun(root, second, nil, advance, archiveOriginals(), "test"); err != nil {
			t.Fatal(err)
		}
		b, err := ReadReviewBatch(root, "batch")
		if err != nil {
			t.Fatal(err)
		}
		if b.PlanID != "" || b.TargetCommit != fix {
			t.Fatalf("unplanned batch changed shape: %+v", b)
		}
	})
}

func TestStalePlanTargetIsReportedAndRepairable(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "stale-target")
	p := readPlanRecord(t, root, gatePlan(t, root, []string{id}, noReviewRequirements()).PlanID)
	planned := p.Batches[0].TargetCommit
	fix := strings.Repeat("c", 40)
	staleGateBatchTarget(t, root, fix)

	_, err := ReviewTaskProgress(root, id)
	if err == nil {
		t.Fatal("stale plan target accepted")
	}
	for _, want := range []string{"plan_target=" + planned, "batch_target=" + fix} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error does not report %s: %v", want, err)
		}
	}
	if problems := CheckReviewGate(root, []string{id}); len(problems) != 1 {
		t.Fatalf("check did not report the drift: %+v", problems)
	}

	x := ReviewPlanExtension{PlanID: p.PlanID, ExpectedRevision: p.Revision, SyncTargets: true, Author: "coordinator", Basis: "对齐旧看板的登记目标"}
	if err = ExtendReviewPlan(root, x); err != nil {
		t.Fatalf("controlled repair rejected: %v", err)
	}
	repaired := readPlanRecord(t, root, p.PlanID)
	if repaired.Batches[0].TargetCommit != fix || repaired.Revision != p.Revision+1 {
		t.Fatalf("repair did not align the record: %+v", repaired)
	}
	if _, err = ReviewTaskProgress(root, id); err != nil {
		t.Fatalf("progress after repair: %v", err)
	}
	var history struct {
		Previous   ReviewPlan          `json:"previous"`
		Request    ReviewPlanExtension `json:"request"`
		RecordedAt string              `json:"recorded_at"`
	}
	if err = WithTransaction(root, reviewScope(nil, true), func(tx *Transaction) error {
		ok, e := readReviewJSON(tx, "plan-history/"+p.PlanID+"/2.json", &history)
		if e == nil && !ok {
			e = reviewError("missing history")
		}
		return e
	}); err != nil {
		t.Fatal(err)
	}
	if history.Previous.Batches[0].TargetCommit != planned {
		t.Fatalf("history lost the previous record: %+v", history.Previous)
	}

	x.ExpectedRevision = repaired.Revision
	if err = ExtendReviewPlan(root, x); err == nil {
		t.Fatal("repair accepted with nothing to sync")
	}
}
