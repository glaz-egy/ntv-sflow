package mock

import "testing"

// Reference values computed with the TypeScript implementation
// (apps/web/src/lib/mock-backend/rng.ts).
func TestRngMatchesTypeScript(t *testing.T) {
	if got := HashString("42|pc-nas-smb|up|exp_core|35"); got != hashTS {
		t.Errorf("HashString = %d, want %d", got, hashTS)
	}
	r := NewRand(12345)
	for i, want := range mulberryTS {
		if got := r.Float(); got != want {
			t.Errorf("mulberry32[%d] = %v, want %v", i, got, want)
		}
	}
	if got := SmoothNoise(42, "bg", 37, 8); got != smoothTS {
		t.Errorf("SmoothNoise = %v, want %v", got, smoothTS)
	}
	if got := Poisson(KeyedRand(7, "x", 3), 12.5); got != poissonSmallTS {
		t.Errorf("Poisson small = %d, want %d", got, poissonSmallTS)
	}
	if got := Poisson(KeyedRand(7, "y", 4), 812.25); got != poissonLargeTS {
		t.Errorf("Poisson large = %d, want %d", got, poissonLargeTS)
	}
}

const (
	hashTS         = 2589613600
	smoothTS       = 0.10524527737470635
	poissonSmallTS = 8
	poissonLargeTS = 816
)

var mulberryTS = []float64{0.9797282677609473, 0.3067522644996643, 0.484205421525985, 0.817934412509203}
