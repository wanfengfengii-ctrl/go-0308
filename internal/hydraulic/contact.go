package hydraulic

import "example.com/potable-water-pipeline/internal/domain"

// ContactTracker integrates disinfectant concentration over logical time to
// produce the concentration-time (CT) product required for effective contact.
// It records the initial concentration, accumulates the rectangular integral
// over each interval, and tracks the terminal concentration and total elapsed
// logical time. All concentration values share a single explicit scale.
type ContactTracker struct {
	concScale int

	started    bool
	startClock int64
	lastClock  int64
	lastConc   int64
	initial    int64
	ct         int64
}

// NewContactTracker returns a tracker for concentrations at the given scale.
func NewContactTracker(concScale int) *ContactTracker {
	return &ContactTracker{concScale: concScale}
}

// Start records the initial concentration at the start clock. It resets any
// prior accumulation and rejects a non-positive clock ordering.
func (c *ContactTracker) Start(clock int64, conc domain.Quantity) error {
	if conc.Scale != c.concScale {
		return ErrScaleMismatch
	}
	if conc.Value < 0 {
		return ErrNegativeValue
	}
	c.started = true
	c.startClock = clock
	c.lastClock = clock
	c.lastConc = conc.Value
	c.initial = conc.Value
	c.ct = 0
	return nil
}

// Sample advances the integral to clock using the concentration observed over
// the interval since the previous sample. A clock that does not advance is a
// regression error; a backward clock is rejected.
func (c *ContactTracker) Sample(clock int64, conc domain.Quantity) error {
	if conc.Scale != c.concScale {
		return ErrScaleMismatch
	}
	if conc.Value < 0 {
		return ErrNegativeValue
	}
	if !c.started {
		return ErrNotStarted
	}
	if clock < c.lastClock {
		return ErrClockRegression
	}
	dt := clock - c.lastClock
	inc, err := mulRaw(dt, conc.Value)
	if err != nil {
		return err
	}
	c.ct, err = addRaw(c.ct, inc)
	if err != nil {
		return err
	}
	c.lastClock = clock
	c.lastConc = conc.Value
	return nil
}

// CT returns the accumulated concentration-time product (at concScale).
func (c *ContactTracker) CT() int64 { return c.ct }

// InitialConc returns the recorded initial concentration value.
func (c *ContactTracker) InitialConc() int64 { return c.initial }

// TerminalConc returns the most recent concentration value.
func (c *ContactTracker) TerminalConc() int64 { return c.lastConc }

// Duration returns the elapsed logical time since Start.
func (c *ContactTracker) Duration() int64 {
	if !c.started {
		return 0
	}
	return c.lastClock - c.startClock
}

// Started reports whether Start has been called.
func (c *ContactTracker) Started() bool { return c.started }

// ContactResult is the outcome of evaluating effective contact against the
// governing thresholds.
type ContactResult struct {
	InitialOK  bool `json:"initial_ok"`
	TerminalOK bool `json:"terminal_ok"`
	DurationOK bool `json:"duration_ok"`
	CTOK       bool `json:"ct_ok"`
}

// Evaluate checks the tracker state against the minimum initial concentration,
// minimum terminal concentration, minimum CT, and required duration. Every
// threshold is checked; a single failure marks the corresponding flag.
func (c *ContactTracker) Evaluate(minInitial, minTerminal domain.Quantity, minCT domain.Quantity, duration int64) ContactResult {
	r := ContactResult{
		InitialOK:  c.started && c.initial >= minInitial.Value,
		TerminalOK: c.started && c.lastConc >= minTerminal.Value,
		DurationOK: c.Duration() >= duration,
		CTOK:       c.ct >= minCT.Value,
	}
	return r
}

// ContactPassed reports whether all contact criteria are satisfied.
func (r ContactResult) ContactPassed() bool {
	return r.InitialOK && r.TerminalOK && r.DurationOK && r.CTOK
}
