package rules

import (
	"sort"

	"example.com/potable-water-pipeline/internal/domain"
)

// Catalog is the versioned rule directory: thresholds, instrument calibration
// rules, and personnel qualifications. Locking a job records the current rule
// digest so a stale summary can never lock a topology.
type Catalog struct {
	current     int
	versions    map[int]domain.RuleVersion
	qual        map[string][]domain.Qualification
	instruments map[string]domain.InstrumentRule
}

// NewCatalog returns an empty catalog. Versions are added via AddVersion.
func NewCatalog() *Catalog {
	return &Catalog{
		versions:    make(map[int]domain.RuleVersion),
		qual:        make(map[string][]domain.Qualification),
		instruments: make(map[string]domain.InstrumentRule),
	}
}

// AddVersion registers a rule version. The highest version number becomes the
// current version.
func (c *Catalog) AddVersion(rv domain.RuleVersion) {
	if rv.Digest == "" {
		rv.Digest = domain.MustDigest(rv)
	}
	c.versions[rv.Version] = rv
	if c.current == 0 || rv.Version > c.current {
		c.current = rv.Version
	}
}

// CurrentVersion returns the highest registered version number, or 0 when the
// catalog is empty.
func (c *Catalog) CurrentVersion() int { return c.current }

// Current returns the current rule version and whether it exists.
func (c *Catalog) Current() (domain.RuleVersion, bool) {
	rv, ok := c.versions[c.current]
	return rv, ok
}

// Version returns the rule version with the given number.
func (c *Catalog) Version(v int) (domain.RuleVersion, bool) {
	rv, ok := c.versions[v]
	return rv, ok
}

// IsCurrent reports whether v is the current rule version.
func (c *Catalog) IsCurrent(v int) bool { return v == c.current }

// AddQualification registers a person's eligibility for a role.
func (c *Catalog) AddQualification(q domain.Qualification) {
	c.qual[q.PersonID] = append(c.qual[q.PersonID], q)
}

// Qualified reports whether person holds the role at the given logical clock.
func (c *Catalog) Qualified(personID, role string, clock int64) bool {
	for _, q := range c.qual[personID] {
		if q.Role == role && q.ValidTo >= clock {
			return true
		}
	}
	return false
}

// AddInstrumentRule registers a calibration rule for an instrument kind.
func (c *Catalog) AddInstrumentRule(r domain.InstrumentRule) {
	c.instruments[r.InstrumentKind] = r
}

// CalibrationDays returns the calibration validity window in days for an
// instrument kind, or 0 when unknown.
func (c *Catalog) CalibrationDays(kind string) int {
	if r, ok := c.instruments[kind]; ok {
		return r.CalibrationDays
	}
	return 0
}

// InstrumentKinds returns the sorted list of registered instrument kinds.
func (c *Catalog) InstrumentKinds() []string {
	kinds := make([]string, 0, len(c.instruments))
	for k := range c.instruments {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	return kinds
}

// DefaultCatalog builds the standard catalog used by the service: version 1
// carries the documented hydraulic and disinfection thresholds, turbidity
// meter and chlorine analyzer calibration rules, and qualified reviewers.
func DefaultCatalog() *Catalog {
	c := NewCatalog()
	c.AddVersion(domain.RuleVersion{
		Version: 1,
		Thresholds: map[string]domain.Quantity{
			"min_flow":          {Value: 500, Scale: 0}, // L/s
			"max_turbidity":     {Value: 5, Scale: 0},   // NTU
			"min_initial_conc":  {Value: 25, Scale: 0},  // mg/L
			"min_terminal_conc": {Value: 10, Scale: 0},  // mg/L
			"min_ct":            {Value: 960, Scale: 0}, // mg*min/L
		},
		Scale: map[string]int{"flow": 0, "conc": 0, "turbidity": 0},
	})
	c.AddInstrumentRule(domain.InstrumentRule{InstrumentKind: "turbidity_meter", CalibrationDays: 90})
	c.AddInstrumentRule(domain.InstrumentRule{InstrumentKind: "chlorine_analyzer", CalibrationDays: 30})
	c.AddQualification(domain.Qualification{PersonID: "reviewer-a", Role: "review", ValidTo: 1 << 40})
	c.AddQualification(domain.Qualification{PersonID: "reviewer-b", Role: "review", ValidTo: 1 << 40})
	c.AddQualification(domain.Qualification{PersonID: "reviewer-c", Role: "review", ValidTo: 1 << 40})
	c.AddQualification(domain.Qualification{PersonID: "inspector-1", Role: "isolation", ValidTo: 1 << 40})
	return c
}
