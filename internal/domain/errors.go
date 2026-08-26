package domain

// ConflictCode is a stable, machine-readable code for idempotency conflicts
// and concurrency failures. Clients rely on these codes being stable.
type ConflictCode string

const (
	ConflictContentMismatch ConflictCode = "content_mismatch"
	ConflictStageOrder      ConflictCode = "stage_order"
	ConflictClockRegression ConflictCode = "clock_regression"
	ConflictRound           ConflictCode = "round_conflict"
	ConflictLeaseExpired    ConflictCode = "lease_expired"
	ConflictLeaseBusy       ConflictCode = "lease_busy"
	ConflictTerminalExists  ConflictCode = "terminal_exists"
)

// ConflictError reports a stable conflict code alongside a human reason.
// Idempotent replays with matching content return the original result;
// mismatched content or a lost race yields a ConflictError.
type ConflictError struct {
	Code   ConflictCode
	Reason string
}

func (e *ConflictError) Error() string {
	if e.Reason == "" {
		return string(e.Code)
	}
	return string(e.Code) + ": " + e.Reason
}

// ValidationError reports an input rejected before persistence.
type ValidationError struct {
	Field  string
	Reason string
}

func (e *ValidationError) Error() string {
	if e.Field == "" {
		return e.Reason
	}
	return e.Field + ": " + e.Reason
}
