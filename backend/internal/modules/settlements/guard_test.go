package settlements

import "testing"

func TestSimulatedPayoutsNeverRunWithRealKeysOrInProduction(t *testing.T) {
	cases := []struct {
		sandbox, production, want bool
	}{
		{sandbox: true, production: false, want: true},   // local development / staging without keys
		{sandbox: false, production: false, want: false}, // real keys, any environment
		{sandbox: true, production: true, want: false},   // production, even if misconfigured
		{sandbox: false, production: true, want: false},
	}
	for _, c := range cases {
		if got := SimulatedPayoutsAllowed(c.sandbox, c.production); got != c.want {
			t.Errorf("sandbox=%v production=%v: got %v, want %v", c.sandbox, c.production, got, c.want)
		}
	}
}
