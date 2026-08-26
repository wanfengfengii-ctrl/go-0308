package domain

// Quantity is a signed fixed-point number with an explicit decimal scale.
// The represented value is Value / 10^Scale. Scale is always non-negative.
// Length, diameter, flow, pressure, concentration, time, and colony counts
// are stored as Quantity (or as bare integers where the documented scale is
// the unit itself). Arithmetic is centralized in the hydraulic package with
// half-away-from-zero rounding and explicit overflow/division checks.
type Quantity struct {
	// Value is the scaled integer representation.
	Value int64 `json:"value"`
	// Scale is the number of decimal places (non-negative).
	Scale int `json:"scale"`
}
