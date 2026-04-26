package entity

import "testing"

func TestDecisionConstants_AreNonEmpty(t *testing.T) {
	cases := []struct {
		name  string
		value string
	}{
		{"DecisionTriggerBarClose", DecisionTriggerBarClose},
		{"DecisionTriggerTickSLTP", DecisionTriggerTickSLTP},
		{"DecisionTriggerTickTrailing", DecisionTriggerTickTrailing},
		{"DecisionRiskApproved", DecisionRiskApproved},
		{"DecisionRiskRejected", DecisionRiskRejected},
		{"DecisionRiskSkipped", DecisionRiskSkipped},
		{"DecisionBookAllowed", DecisionBookAllowed},
		{"DecisionBookVetoed", DecisionBookVetoed},
		{"DecisionBookSkipped", DecisionBookSkipped},
		{"DecisionOrderFilled", DecisionOrderFilled},
		{"DecisionOrderFailed", DecisionOrderFailed},
		{"DecisionOrderNoop", DecisionOrderNoop},
		{"RejectedStageRisk", RejectedStageRisk},
		{"RejectedStageBookGate", RejectedStageBookGate},
	}
	for _, c := range cases {
		if c.value == "" {
			t.Errorf("%s must not be empty", c.name)
		}
	}
}

func TestDecisionRecord_ZeroValueIsValid(t *testing.T) {
	var r DecisionRecord
	if r.SignalAction != "" || r.SequenceInBar != 0 {
		t.Errorf("zero value should be all-zero, got %+v", r)
	}
}
