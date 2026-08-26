package sample

import (
	"fmt"
	"sort"

	"example.com/potable-water-pipeline/internal/domain"
)

// Custody actions that make up a complete sample chain. The documented chain
// is collect -> seal -> handoff -> receipt and must be complete before a lab
// result may form a conclusion.
const (
	ActionCollect = "collect"
	ActionSeal    = "seal"
	ActionHandoff = "handoff"
	ActionReceipt = "receipt"
)

// GenerateLabel builds a unique, non-reusable sample label from the job,
// sampling point, round, and a monotonically increasing sequence number. The
// label embeds the point and round so duplicate collection in the same round
// is impossible.
func GenerateLabel(job domain.JobID, point domain.SamplePointID, round, seq int) string {
	return fmt.Sprintf("%s-%s-r%d-%d", job, point, round, seq)
}

// SampleDigest returns the canonical digest of a sample's identity: its job,
// point, round, and label. The digest is recorded on the sample and must match
// any lab receipt before a conclusion may form.
func SampleDigest(s domain.Sample) string {
	return domain.MustDigest(struct {
		Job   domain.JobID         `json:"job"`
		Point domain.SamplePointID `json:"point"`
		Round int                  `json:"round"`
		Label string               `json:"label"`
	}{s.JobID, s.PointID, s.Round, s.Label})
}

// Chain is the ordered custody trail for a single sample.
type Chain []domain.CustodyEvent

// Sorted returns the custody events in non-decreasing logical-time order.
func (c Chain) Sorted() Chain {
	out := append(Chain(nil), c...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Clock != out[j].Clock {
			return out[i].Clock < out[j].Clock
		}
		return out[i].Action < out[j].Action
	})
	return out
}

// Complete reports whether the chain contains all four required actions in
// order (collect, seal, handoff, receipt) with non-decreasing clocks.
func (c Chain) Complete() bool {
	ordered := c.Sorted()
	seen := map[string]bool{}
	var lastClock int64 = -1
	for _, e := range ordered {
		if e.Clock < lastClock {
			return false
		}
		lastClock = e.Clock
		seen[e.Action] = true
	}
	for _, a := range []string{ActionCollect, ActionSeal, ActionHandoff, ActionReceipt} {
		if !seen[a] {
			return false
		}
	}
	return true
}

// ValidateCustody returns an error describing why the chain is incomplete.
func ValidateCustody(c Chain) error {
	if len(c) == 0 {
		return fmt.Errorf("custody chain is empty")
	}
	if !c.Complete() {
		return fmt.Errorf("custody chain incomplete: requires collect, seal, handoff, receipt in order")
	}
	return nil
}
