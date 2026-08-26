package hydraulic

import "example.com/potable-water-pipeline/internal/domain"

// Rational approximation of pi used for integer volume calculations.
const (
	piNum = 355
	piDen = 113
)

// VolumeLitres returns the internal pipeline volume in litres for a section
// of the given integer millimetre diameter and integer metre length. The
// result is rounded half-away-from-zero and returned as an integer litre
// count. Negative inputs are rejected.
//
// Volume derives from V = pi * (d/2)^2 * L, converted from mm^3 to litres:
//
//	V(L) = pi * d^2 * L / 4000 = 355 * d^2 * L / (113 * 4000)
func VolumeLitres(diameterMM, lengthM int) (int, error) {
	if diameterMM < 0 || lengthM < 0 {
		return 0, ErrNegativeValue
	}
	d2, err := mulRaw(int64(diameterMM), int64(diameterMM))
	if err != nil {
		return 0, err
	}
	num, err := mulRaw(d2, int64(lengthM))
	if err != nil {
		return 0, err
	}
	num, err = mulRaw(num, piNum)
	if err != nil {
		return 0, err
	}
	const den = int64(piDen) * 4000 // 452000
	q, r, err := divRem(num, den)
	if err != nil {
		return 0, err
	}
	if roundAway(num, den, r) {
		q++
	}
	return int(q), nil
}

// TurnoverFactor returns how many pipeline volumes have passed through a
// section given a cumulative flow volume and the section volume, expressed at
// the requested scale with half-away-from-zero rounding.
func TurnoverFactor(flowLitres, volumeLitres int, scale int) (int64, error) {
	if volumeLitres == 0 {
		return 0, ErrDivideByZero
	}
	if scale < 0 {
		return 0, ErrNegativeScale
	}
	a := domain.Quantity{Value: int64(flowLitres), Scale: 0}
	b := domain.Quantity{Value: int64(volumeLitres), Scale: 0}
	q, err := Div(a, b, scale)
	if err != nil {
		return 0, err
	}
	return q.Value, nil
}
