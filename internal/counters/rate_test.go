package counters

import "testing"

func TestCompute(t *testing.T) {
	r := func(at, oct float64, w int) Reading { return Reading{At: at, Octets: oct, Width: w} }
	p := func(x Reading) *Reading { return &x }
	cases := []struct {
		name string
		prev *Reading
		cur  Reading
		opt  Options
		want Rate
	}{
		{"basic", p(r(100, 1000, 64)), r(110, 126000, 64), Options{}, Rate{OK: true, Bps: 100000, IntervalSeconds: 10}},
		{"first sample", nil, r(1, 5, 64), Options{}, Rate{Reason: FirstSample}},
		{"zero interval", p(r(10, 1, 64)), r(10, 2, 64), Options{}, Rate{Reason: NonPositiveInterval}},
		{"32-bit wrap", p(r(0, twoPow32-1000, 32)), r(1, 24000, 32), Options{}, Rate{OK: true, Bps: 25000 * 8, IntervalSeconds: 1}},
		{"64-bit reset", p(r(0, 5e12, 64)), r(20, 1000, 64), Options{}, Rate{Reason: CounterReset}},
		{"implausible wrap", p(r(0, 3e9, 32)), r(1, 2e9, 32), Options{IfSpeedBps: 1e9}, Rate{Reason: CounterReset}},
		{"stale previous", p(r(0, 0, 64)), r(600, 1000, 64), Options{MaxIntervalSeconds: 120}, Rate{Reason: StalePrevious}},
	}
	for _, tc := range cases {
		if got := Compute(tc.prev, tc.cur, tc.opt); got != tc.want {
			t.Errorf("%s: got %+v want %+v", tc.name, got, tc.want)
		}
	}
}
