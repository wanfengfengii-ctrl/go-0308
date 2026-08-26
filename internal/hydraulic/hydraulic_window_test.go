package hydraulic

import (
	"errors"
	"testing"
)

func TestWindowAccumulatesAndResets(t *testing.T) {
	var w Window
	if got := w.Record(true); got != 1 {
		t.Fatalf("first compliant count = %d, want 1", got)
	}
	if got := w.Record(true); got != 2 {
		t.Fatalf("second compliant count = %d, want 2", got)
	}
	if got := w.Record(false); got != 0 {
		t.Fatalf("violation count = %d, want 0", got)
	}
	if got := w.Record(true); got != 1 {
		t.Fatalf("post-reset count = %d, want 1", got)
	}
	if !w.Meets(1) {
		t.Fatal("window should meet threshold 1")
	}
	if w.Meets(2) {
		t.Fatal("window should not meet threshold 2")
	}
}

func TestWindowReset(t *testing.T) {
	var w Window
	w.Record(true)
	w.Record(true)
	w.Reset()
	if w.Count() != 0 {
		t.Fatalf("Count after Reset = %d, want 0", w.Count())
	}
}

func TestVolumeLitres(t *testing.T) {
	// V = pi * d^2 * L / 4000. d=100mm, L=1000m.
	got, err := VolumeLitres(100, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if got != 7854 {
		t.Fatalf("VolumeLitres(100,1000) = %d, want 7854", got)
	}
}

func TestVolumeLitresNegativeRejected(t *testing.T) {
	if _, err := VolumeLitres(-1, 10); !errors.Is(err, ErrNegativeValue) {
		t.Fatalf("negative diameter err = %v, want ErrNegativeValue", err)
	}
	if _, err := VolumeLitres(10, -1); !errors.Is(err, ErrNegativeValue) {
		t.Fatalf("negative length err = %v, want ErrNegativeValue", err)
	}
}

func TestTurnoverFactor(t *testing.T) {
	got, err := TurnoverFactor(10000, 5000, 1) // 2.0 -> value 20 at scale 1
	if err != nil {
		t.Fatal(err)
	}
	if got != 20 {
		t.Fatalf("TurnoverFactor = %d, want 20", got)
	}
}

func TestTurnoverFactorZeroVolume(t *testing.T) {
	if _, err := TurnoverFactor(100, 0, 0); !errors.Is(err, ErrDivideByZero) {
		t.Fatalf("zero volume err = %v, want ErrDivideByZero", err)
	}
}

func TestConcentrationTime(t *testing.T) {
	got := ConcentrationTime(0, 10, 3) // 10 * 3
	if got != 30 {
		t.Fatalf("ConcentrationTime = %d, want 30", got)
	}
}

func TestDoseAmount(t *testing.T) {
	// 10000 L at 25 mg/L = 250000 mg, concentration scale 0.
	got, err := DoseAmount(10000, q(25, 0))
	if err != nil {
		t.Fatal(err)
	}
	if got.Value != 250000 || got.Scale != 0 {
		t.Fatalf("DoseAmount = %+v, want {250000 0}", got)
	}
	if _, err := DoseAmount(-1, q(25, 0)); !errors.Is(err, ErrNegativeValue) {
		t.Fatalf("negative volume err = %v, want ErrNegativeValue", err)
	}
}

func TestRequiredVolume(t *testing.T) {
	// turnover factor 2.0 (scale 1) over 5000 L = 10000 L.
	got, err := RequiredVolume(20, 1, 5000)
	if err != nil {
		t.Fatal(err)
	}
	if got != 10000 {
		t.Fatalf("RequiredVolume = %d, want 10000", got)
	}
	if !MetTurnover(12000, got) {
		t.Fatal("should meet turnover")
	}
	if MetTurnover(9999, got) {
		t.Fatal("should not meet turnover")
	}
}

func TestRequiredVolumeRounding(t *testing.T) {
	// turnover factor 0.5 (5 at scale 1) over 3 L = 1.5 L -> rounds to 2.
	got, err := RequiredVolume(5, 1, 3)
	if err != nil {
		t.Fatal(err)
	}
	if got != 2 {
		t.Fatalf("RequiredVolume(0.5, 3) = %d, want 2", got)
	}
}

func TestContactTracker(t *testing.T) {
	ct := NewContactTracker(0)
	if err := ct.Start(0, q(30, 0)); err != nil {
		t.Fatal(err)
	}
	if err := ct.Sample(10, q(25, 0)); err != nil {
		t.Fatal(err)
	}
	if err := ct.Sample(20, q(20, 0)); err != nil {
		t.Fatal(err)
	}
	// CT = 25*10 + 20*10 = 450.
	if ct.CT() != 450 {
		t.Fatalf("CT = %d, want 450", ct.CT())
	}
	if ct.InitialConc() != 30 || ct.TerminalConc() != 20 {
		t.Fatalf("initial/terminal = %d/%d, want 30/20", ct.InitialConc(), ct.TerminalConc())
	}
	if ct.Duration() != 20 {
		t.Fatalf("duration = %d, want 20", ct.Duration())
	}
	res := ct.Evaluate(q(25, 0), q(10, 0), q(450, 0), 15)
	if !res.ContactPassed() {
		t.Fatalf("contact should pass, got %+v", res)
	}
}

func TestContactTrackerClockRegression(t *testing.T) {
	ct := NewContactTracker(0)
	if err := ct.Start(10, q(30, 0)); err != nil {
		t.Fatal(err)
	}
	if err := ct.Sample(5, q(20, 0)); !errors.Is(err, ErrClockRegression) {
		t.Fatalf("regression err = %v, want ErrClockRegression", err)
	}
}
