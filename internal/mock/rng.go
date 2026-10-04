// Package mock is the Go port of the deterministic mock traffic engine
// (apps/web/src/lib/mock-backend). Outputs must match the TypeScript
// implementation for the same (seed, scenario, tick) — see golden_test.go.
//
// Portability rules (D-039):
//   - integer hashing/PRNG is bit-identical to the JS (uint32 arithmetic);
//   - only correctly rounded float ops (+ − × ÷ √) plus math.Exp are used;
//   - every product feeding an addition is wrapped in float64(...) so the
//     Go compiler cannot fuse it into an FMA (which JS never does).
package mock

import (
	"math"
	"strconv"
	"strings"
)

// HashString is FNV-1a 32-bit over the string's (ASCII) bytes.
func HashString(s string) uint32 {
	h := uint32(0x811c9dc5)
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 0x01000193
	}
	return h
}

// Rand is mulberry32.
type Rand struct{ a uint32 }

func NewRand(seed uint32) *Rand { return &Rand{a: seed} }

// Float returns a float in [0, 1).
func (r *Rand) Float() float64 {
	r.a += 0x6d2b79f5
	t := r.a
	t = (t ^ (t >> 15)) * (t | 1)
	t ^= t + (t^(t>>7))*(t|61)
	return float64(t^(t>>14)) / 4294967296
}

// KeyedRand mirrors keyedRng(seed, ...parts): parts must be strings or ints.
func KeyedRand(seed int, parts ...any) *Rand {
	var b strings.Builder
	b.WriteString(strconv.Itoa(seed))
	b.WriteByte('|')
	for i, p := range parts {
		if i > 0 {
			b.WriteByte('|')
		}
		switch v := p.(type) {
		case string:
			b.WriteString(v)
		case int:
			b.WriteString(strconv.Itoa(v))
		default:
			panic("KeyedRand: parts must be string or int")
		}
	}
	return NewRand(HashString(b.String()))
}

func KeyedUniform(seed int, parts ...any) float64 { return KeyedRand(seed, parts...).Float() }

// Gaussian approximates a standard normal (Irwin–Hall, 12 uniforms).
func Gaussian(r *Rand) float64 {
	sum := 0.0
	for i := 0; i < 12; i++ {
		sum += r.Float()
	}
	return sum - 6
}

// Poisson draws a Poisson-distributed integer with mean lambda.
func Poisson(r *Rand, lambda float64) int {
	if lambda <= 0 {
		return 0
	}
	if lambda < 40 {
		limit := math.Exp(-lambda)
		k := 0
		p := r.Float()
		for p > limit {
			k++
			p *= r.Float()
		}
		return k
	}
	v := math.Floor(lambda + float64(math.Sqrt(lambda)*Gaussian(r)) + 0.5)
	if v < 0 {
		return 0
	}
	return int(v)
}

// SmoothNoise is value noise in [-1, 1], knots every `period` seconds,
// smoothstep-interpolated.
func SmoothNoise(seed int, key string, t, period int) float64 {
	x := float64(t) / float64(period)
	i := math.Floor(x)
	f := x - i
	a := float64(KeyedUniform(seed, key, int(i))*2) - 1
	b := float64(KeyedUniform(seed, key, int(i)+1)*2) - 1
	w := f * f * (3 - float64(2*f))
	return float64(a*(1-w)) + float64(b*w)
}
