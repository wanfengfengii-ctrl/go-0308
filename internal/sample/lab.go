package sample

import (
	"fmt"

	"example.com/potable-water-pipeline/internal/domain"
)

// Lab status values for a scripted, deterministic detection attempt.
const (
	StatusPending   = "pending"
	StatusRetryable = "retryable"
	StatusComplete  = "complete"
)

// CalibratedWithin reports whether an instrument calibrated at calibratedAt is
// still within its validity window of days at the given logical clock. A
// zero or negative window always fails.
func CalibratedWithin(calibratedAt, now int64, days int) bool {
	if days <= 0 {
		return false
	}
	if calibratedAt > now {
		return false
	}
	return now-calibratedAt < int64(days)
}

// NewAttempt creates a pending detection attempt for a sample. The attempt
// digest records the sample digest plus the requested test item so that a
// later receipt must match both.
func NewAttempt(id string, s domain.Sample, testItem string) domain.LabAttempt {
	return domain.LabAttempt{
		ID:          id,
		SampleID:    s.ID,
		RetryNumber: 0,
		Status:      StatusPending,
		Calibration: "unverified",
		Digest: domain.MustDigest(struct {
			Sample   string `json:"sample"`
			TestItem string `json:"test_item"`
		}{s.Digest, testItem}),
	}
}

// Retryable reports whether an attempt may be retried: it must not already be
// complete and its retry number must be below the given ceiling.
func Retryable(a domain.LabAttempt, maxRetries int) bool {
	return a.Status != StatusComplete && a.RetryNumber < maxRetries
}

// ResultDecision validates a lab receipt against its attempt and the governing
// calibration and content rules. It returns the test item, the passed flag,
// and an error describing the first violation.
type ResultDecision struct {
	Passed bool
	Item   string
}

// ValidateResult checks that a lab result matches the attempt digest, test
// item, calibration status, and sample. A mismatch or stale calibration means
// the result cannot form a conclusion; it is recorded as retryable history.
func ValidateResult(a domain.LabAttempt, s domain.Sample, testItem string, calibrated bool, value domain.Quantity, passed bool) (ResultDecision, error) {
	want := domain.MustDigest(struct {
		Sample   string `json:"sample"`
		TestItem string `json:"test_item"`
	}{s.Digest, testItem})
	if a.Digest != want {
		return ResultDecision{}, fmt.Errorf("receipt summary mismatch")
	}
	if testItem == "" {
		return ResultDecision{}, fmt.Errorf("missing test item")
	}
	if !calibrated {
		return ResultDecision{}, fmt.Errorf("instrument calibration stale")
	}
	if value.Value < 0 {
		return ResultDecision{}, fmt.Errorf("negative result value")
	}
	return ResultDecision{Passed: passed, Item: testItem}, nil
}
