package hydraulic

// Window tracks a continuous compliance window over logical time. A reading
// that satisfies the governing predicate extends the window; a failing
// reading resets it to zero. The caller combines per-metric predicates (for
// example, minimum flow AND maximum turbidity) before recording.
type Window struct {
	count int64
}

// Record extends or resets the window based on the compliance of the current
// reading and returns the new consecutive compliant count.
func (w *Window) Record(compliant bool) int64 {
	if compliant {
		w.count++
		return w.count
	}
	w.count = 0
	return 0
}

// Count returns the current consecutive compliant reading count.
func (w *Window) Count() int64 { return w.count }

// Reset clears the window to zero.
func (w *Window) Reset() { w.count = 0 }

// Meets reports whether the window has accumulated at least min consecutive
// compliant readings.
func (w *Window) Meets(min int64) bool { return w.count >= min }

// ConcentrationTime integrates concentration over the contact period using a
// rectangular rule: it sums concentration * dt for each step where dt is the
// logical-time delta since the previous sample. It returns the integral at
// the product scale (concentration scale + time scale).
func ConcentrationTime(prev, curr int64, concentration int64) int64 {
	return (curr - prev) * concentration
}
