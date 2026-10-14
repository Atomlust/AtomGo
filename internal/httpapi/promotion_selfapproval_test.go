package httpapi

import (
	"errors"
	"testing"

	"github.com/atomlust/atomgo/internal/promotion"
)

// TestEvaluateApprovalDecision_SelfApprovalForbidden 验证禁止自审:批准者==发起人 → 拒绝。
func TestEvaluateApprovalDecision_SelfApprovalForbidden(t *testing.T) {
	info := promotion.GateInfo{InitiatedBy: "alice", Approvers: []string{"alice", "bob"}}
	err := evaluateApprovalDecision("alice", info)
	if !errors.Is(err, promotion.ErrSelfApproval) {
		t.Fatalf("self-approval err = %v, want ErrSelfApproval", err)
	}
}

// TestEvaluateApprovalDecision_OtherApproverAllowed 验证非发起人且在名单内 → 放行。
func TestEvaluateApprovalDecision_OtherApproverAllowed(t *testing.T) {
	info := promotion.GateInfo{InitiatedBy: "alice", Approvers: []string{"bob", "carol"}}
	if err := evaluateApprovalDecision("bob", info); err != nil {
		t.Fatalf("bob approving should be allowed, got %v", err)
	}
}

// TestEvaluateApprovalDecision_NotInList 验证批准者不在名单内 → ErrApproverNotAllowed(先于自审判定)。
func TestEvaluateApprovalDecision_NotInList(t *testing.T) {
	info := promotion.GateInfo{InitiatedBy: "alice", Approvers: []string{"bob"}}
	err := evaluateApprovalDecision("carol", info)
	if !errors.Is(err, promotion.ErrApproverNotAllowed) {
		t.Fatalf("carol not in list err = %v, want ErrApproverNotAllowed", err)
	}
}

// TestEvaluateApprovalDecision_EmptyApproversNoSelf 验证无名单时不限定身份,但自审仍禁止。
func TestEvaluateApprovalDecision_EmptyApproversNoSelf(t *testing.T) {
	// 无名单 + 不同人 → 放行(向后兼容)。
	ok := promotion.GateInfo{InitiatedBy: "alice", Approvers: nil}
	if err := evaluateApprovalDecision("bob", ok); err != nil {
		t.Fatalf("empty approvers, different actor should pass, got %v", err)
	}
	// 无名单 + 自审 → 仍拒绝(职责分离)。
	self := promotion.GateInfo{InitiatedBy: "alice", Approvers: nil}
	if err := evaluateApprovalDecision("alice", self); !errors.Is(err, promotion.ErrSelfApproval) {
		t.Fatalf("empty approvers, self-approval err = %v, want ErrSelfApproval", err)
	}
}

// TestEvaluateApprovalDecision_NoInitiatorSkipsSelfCheck 验证无发起人(系统)时不做自审判定。
func TestEvaluateApprovalDecision_NoInitiatorSkipsSelfCheck(t *testing.T) {
	info := promotion.GateInfo{InitiatedBy: "", Approvers: nil}
	if err := evaluateApprovalDecision("admin", info); err != nil {
		t.Fatalf("no initiator should skip self-check, got %v", err)
	}
}
