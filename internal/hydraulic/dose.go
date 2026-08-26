package hydraulic

import "example.com/potable-water-pipeline/internal/domain"

// DoseAmount computes the exact disinfectant mass required to bring a pipeline
// of the given integer litre volume to the requested concentration. The result
// is expressed at the concentration scale (mass = volume_L * concentration).
// A zero or negative volume is rejected; the product is checked for overflow.
func DoseAmount(volumeLitres int, concentration domain.Quantity) (domain.Quantity, error) {
	if volumeLitres < 0 {
		return domain.Quantity{}, ErrNegativeValue
	}
	if concentration.Value < 0 {
		return domain.Quantity{}, ErrNegativeValue
	}
	v := domain.Quantity{Value: int64(volumeLitres), Scale: 0}
	return Mul(v, concentration)
}

// RequiredVolume computes the pipeline volume, in integer litres, that must be
// flushed to achieve the target turnover (replacement) factor. The turnover is
// a fixed-point factor at turnoverScale (for example, 10 at scale 1 means 1.0
// volumes). The product is rounded half-away-from-zero to whole litres.
func RequiredVolume(turnover int64, turnoverScale int, volumeLitres int) (int, error) {
	if turnover <= 0 || volumeLitres <= 0 {
		return 0, ErrNegativeValue
	}
	if turnoverScale < 0 {
		return 0, ErrNegativeScale
	}
	factor := domain.Quantity{Value: turnover, Scale: turnoverScale}
	vol := domain.Quantity{Value: int64(volumeLitres), Scale: 0}
	prod, err := Mul(factor, vol)
	if err != nil {
		return 0, err
	}
	// Convert the product back to whole litres (scale 0) with rounding.
	litres, err := Div(prod, domain.Quantity{Value: 1, Scale: 0}, 0)
	if err != nil {
		return 0, err
	}
	if litres.Value > int64(^uint(0)>>1) {
		return 0, ErrOverflow
	}
	return int(litres.Value), nil
}

// MetTurnover reports whether a cumulative flow volume in whole litres meets
// the required replacement volume.
func MetTurnover(cumulativeFlow, required int) bool {
	return cumulativeFlow >= required
}
