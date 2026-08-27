package hydraulic

import (
	"errors"
	"testing"

	"example.com/potable-water-pipeline/internal/domain"
)

func q(v int64, scale int) domain.Quantity {
	return domain.Quantity{Value: v, Scale: scale}
}

func TestAddSub(t *testing.T) {
	got, err := Add(q(1000, 2), q(250, 2))
	if err != nil {
		t.Fatal(err)
	}
	if got.Value != 1250 || got.Scale != 2 {
		t.Fatalf("Add = %+v, want {1250 2}", got)
	}

	got, err = Sub(q(1000, 2), q(250, 2))
	if err != nil {
		t.Fatal(err)
	}
	if got.Value != 750 || got.Scale != 2 {
		t.Fatalf("Sub = %+v, want {750 2}", got)
	}
}

func TestAddScaleMismatch(t *testing.T) {
	if _, err := Add(q(1, 0), q(1, 1)); !errors.Is(err, ErrScaleMismatch) {
		t.Fatalf("Add scale mismatch err = %v, want ErrScaleMismatch", err)
	}
}

func TestMul(t *testing.T) {
	got, err := Mul(q(150, 1), q(20, 2)) // 15.0 * 0.20 = 3.0
	if err != nil {
		t.Fatal(err)
	}
	if got.Value != 3000 || got.Scale != 3 {
		t.Fatalf("Mul = %+v, want {3000 3}", got)
	}
}

func TestDivExact(t *testing.T) {
	got, err := Div(q(1000, 0), q(4, 0), 0) // 1000/4 = 250
	if err != nil {
		t.Fatal(err)
	}
	if got.Value != 250 || got.Scale != 0 {
		t.Fatalf("Div = %+v, want {250 0}", got)
	}
}

func TestDivRoundHalfAwayPositive(t *testing.T) {
	// 1/3 at scale 2 = 0.333... -> 33 (0.33), since 0.333 rounds to 0.33.
	got, err := Div(q(1, 0), q(3, 0), 2)
	if err != nil {
		t.Fatal(err)
	}
	if got.Value != 33 || got.Scale != 2 {
		t.Fatalf("Div(1/3) = %+v, want {33 2}", got)
	}
	// 2/3 at scale 2 = 0.666... -> 67 (0.67).
	got, err = Div(q(2, 0), q(3, 0), 2)
	if err != nil {
		t.Fatal(err)
	}
	if got.Value != 67 || got.Scale != 2 {
		t.Fatalf("Div(2/3) = %+v, want {67 2}", got)
	}
}

func TestDivRoundHalfAwayNegative(t *testing.T) {
	// -1/3 at scale 2 = -0.333... -> -33 (half away from zero).
	got, err := Div(q(-1, 0), q(3, 0), 2)
	if err != nil {
		t.Fatal(err)
	}
	if got.Value != -33 || got.Scale != 2 {
		t.Fatalf("Div(-1/3) = %+v, want {-33 2}", got)
	}
}

func TestDivByZero(t *testing.T) {
	if _, err := Div(q(1, 0), q(0, 0), 0); !errors.Is(err, ErrDivideByZero) {
		t.Fatalf("Div by zero err = %v, want ErrDivideByZero", err)
	}
}

func TestMulOverflow(t *testing.T) {
	big := q(1<<40, 0)
	if _, err := Mul(big, big); !errors.Is(err, ErrOverflow) {
		t.Fatalf("Mul overflow err = %v, want ErrOverflow", err)
	}
}

func TestCmp(t *testing.T) {
	c, err := Cmp(q(150, 1), q(200, 1))
	if err != nil || c != -1 {
		t.Fatalf("Cmp(150,200) = %d, %v; want -1", c, err)
	}
	c, err = Cmp(q(15, 0), q(150, 1)) // 15 == 15.0
	if err != nil || c != 0 {
		t.Fatalf("Cmp(15, 15.0) = %d, %v; want 0", c, err)
	}
	c, err = Cmp(q(2, 0), q(150, 2)) // 2 > 1.50
	if err != nil || c != 1 {
		t.Fatalf("Cmp(2, 1.50) = %d, %v; want 1", c, err)
	}
}
