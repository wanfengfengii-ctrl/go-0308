package hydraulic

import (
	"errors"
	"math"

	"example.com/potable-water-pipeline/internal/domain"
)

// Fixed-point arithmetic errors. Calculations return these without falling
// back to floating point or truncating results.
var (
	ErrScaleMismatch   = errors.New("scale mismatch")
	ErrDivideByZero    = errors.New("divide by zero")
	ErrOverflow        = errors.New("fixed-point overflow")
	ErrNegativeScale   = errors.New("negative scale")
	ErrNegativeValue   = errors.New("negative value")
	ErrNotStarted      = errors.New("contact tracker not started")
	ErrClockRegression = errors.New("logical clock regression")
)

// addRaw adds two int64 values, failing on signed overflow.
func addRaw(a, b int64) (int64, error) {
	s := a + b
	if (b > 0 && s < a) || (b < 0 && s > a) {
		return 0, ErrOverflow
	}
	return s, nil
}

// subRaw subtracts b from a, failing on signed overflow.
func subRaw(a, b int64) (int64, error) {
	d := a - b
	if (b < 0 && d < a) || (b > 0 && d > a) {
		return 0, ErrOverflow
	}
	return d, nil
}

// mulRaw multiplies two int64 values, failing on signed overflow.
func mulRaw(a, b int64) (int64, error) {
	if a == 0 || b == 0 {
		return 0, nil
	}
	// math.MinInt64 * -1 cannot be represented even though the naive
	// r/b check below would pass for the wrapped result.
	if (a == -1 && b == math.MinInt64) || (b == -1 && a == math.MinInt64) {
		return 0, ErrOverflow
	}
	r := a * b
	if r/b != a {
		return 0, ErrOverflow
	}
	return r, nil
}

// Cmp compares two quantities. It returns -1, 0, or 1.
func Cmp(a, b domain.Quantity) (int, error) {
	if a.Scale == b.Scale {
		return cmpValues(a.Value, b.Value), nil
	}
	if a.Scale < b.Scale {
		av, err := rescaleUp(a.Value, b.Scale-a.Scale)
		if err != nil {
			return 0, err
		}
		return cmpValues(av, b.Value), nil
	}
	bv, err := rescaleUp(b.Value, a.Scale-b.Scale)
	if err != nil {
		return 0, err
	}
	return cmpValues(a.Value, bv), nil
}

func cmpValues(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// Add returns a + b. Scales must match.
func Add(a, b domain.Quantity) (domain.Quantity, error) {
	if a.Scale != b.Scale {
		return domain.Quantity{}, ErrScaleMismatch
	}
	v, err := addRaw(a.Value, b.Value)
	if err != nil {
		return domain.Quantity{}, err
	}
	return domain.Quantity{Value: v, Scale: a.Scale}, nil
}

// Sub returns a - b. Scales must match.
func Sub(a, b domain.Quantity) (domain.Quantity, error) {
	if a.Scale != b.Scale {
		return domain.Quantity{}, ErrScaleMismatch
	}
	v, err := subRaw(a.Value, b.Value)
	if err != nil {
		return domain.Quantity{}, err
	}
	return domain.Quantity{Value: v, Scale: a.Scale}, nil
}

// Mul returns a * b with the resulting scale equal to a.Scale + b.Scale.
func Mul(a, b domain.Quantity) (domain.Quantity, error) {
	v, err := mulRaw(a.Value, b.Value)
	if err != nil {
		return domain.Quantity{}, err
	}
	scale := a.Scale + b.Scale
	if scale < 0 {
		return domain.Quantity{}, ErrNegativeScale
	}
	return domain.Quantity{Value: v, Scale: scale}, nil
}

// Div returns a / b rescaled to targetScale using half-away-from-zero
// rounding. b must be non-zero.
func Div(a, b domain.Quantity, targetScale int) (domain.Quantity, error) {
	if b.Value == 0 {
		return domain.Quantity{}, ErrDivideByZero
	}
	if targetScale < 0 {
		return domain.Quantity{}, ErrNegativeScale
	}
	// Compute a/b in integer arithmetic by scaling the numerator so that the
	// quotient lands on the target scale, then round half-away-from-zero.
	shift := targetScale - a.Scale + b.Scale
	if shift >= 0 {
		num, err := rescaleUp(a.Value, shift)
		if err != nil {
			return domain.Quantity{}, err
		}
		return divRound(num, b.Value, targetScale)
	}
	den, err := rescaleUp(b.Value, -shift)
	if err != nil {
		return domain.Quantity{}, err
	}
	return divRound(a.Value, den, targetScale)
}

// divRound divides num by den and rounds half-away-from-zero.
func divRound(num, den int64, targetScale int) (domain.Quantity, error) {
	q, r, err := divRem(num, den)
	if err != nil {
		return domain.Quantity{}, err
	}
	if r != 0 {
		if roundAway(num, den, r) {
			if (num > 0) == (den > 0) {
				q, err = addRaw(q, 1)
			} else {
				q, err = subRaw(q, 1)
			}
			if err != nil {
				return domain.Quantity{}, err
			}
		}
	}
	return domain.Quantity{Value: q, Scale: targetScale}, nil
}

// roundAway reports whether the division remainder r should round away from
// zero, i.e. whether |r|*2 >= |den|. The comparison is performed in uint64 to
// avoid overflow for magnitudes near math.MaxInt64.
func roundAway(num, den, r int64) bool {
	ur := uint64(r)
	if r < 0 {
		ur = uint64(-r)
	}
	uden := uint64(den)
	if den < 0 {
		uden = uint64(-den)
	}
	return ur*2 >= uden
}

// divRem returns quotient and remainder with truncation toward zero.
func divRem(num, den int64) (q, r int64, err error) {
	if den == 0 {
		return 0, 0, ErrDivideByZero
	}
	if num == math.MinInt64 && den == -1 {
		return 0, 0, ErrOverflow
	}
	q = num / den
	r = num % den
	return q, r, nil
}

// rescaleUp multiplies v by 10^shift, failing on overflow.
func rescaleUp(v int64, shift int) (int64, error) {
	if shift < 0 {
		return 0, ErrNegativeScale
	}
	out := v
	for i := 0; i < shift; i++ {
		m, err := mulRaw(out, 10)
		if err != nil {
			return 0, err
		}
		out = m
	}
	return out, nil
}
