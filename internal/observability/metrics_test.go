package observability

import "testing"

// TestPoolGaugesRegistered asserts the four pgxpool_* gauges are registered
// under exactly the bare names the dashboards and the DbPoolSaturation alert
// reference. If any name drifts, the previously-inert pool panels/alerts go
// silent again, so this test pins the contract. It gathers from the registry
// directly (no testutil dep) to keep go.mod untouched.
func TestPoolGaugesRegistered(t *testing.T) {
	m := NewMetrics()

	// Set non-default values so we can assert the emitted sample values.
	m.PoolAcquiredConns.Set(3)
	m.PoolIdleConns.Set(2)
	m.PoolTotalConns.Set(5)
	m.PoolMaxConns.Set(10)

	want := map[string]float64{
		"pgxpool_acquired_conns": 3,
		"pgxpool_idle_conns":     2,
		"pgxpool_total_conns":    5,
		"pgxpool_max_conns":      10,
	}

	families, err := m.Registry.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	got := map[string]float64{}
	for _, mf := range families {
		if _, ok := want[mf.GetName()]; !ok {
			continue
		}
		ms := mf.GetMetric()
		if len(ms) != 1 {
			t.Fatalf("%s: expected 1 sample, got %d", mf.GetName(), len(ms))
		}
		got[mf.GetName()] = ms[0].GetGauge().GetValue()
	}

	for name, val := range want {
		v, ok := got[name]
		if !ok {
			t.Errorf("gauge %q not exposed by registry", name)
			continue
		}
		if v != val {
			t.Errorf("gauge %q = %v, want %v", name, v, val)
		}
	}
}

// TestObservePoolNilSafe confirms ObservePool tolerates a nil pool/wrapper so
// the observe loop never panics before the DB is wired.
func TestObservePoolNilSafe(t *testing.T) {
	m := NewMetrics()
	m.ObservePool(nil) // nil *database.Pool

	var nilMetrics *Metrics
	nilMetrics.ObservePool(nil) // nil receiver
}
