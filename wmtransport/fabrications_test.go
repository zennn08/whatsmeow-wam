package wmtransport

import (
	"testing"

	wam "github.com/zennn08/whatsmeow-wam"
)

// TestFabricationsResolveAllEnums runs every ambient fab many times (to exercise
// its random branches) and asserts every committed event is known and every
// enum-typed field carries a valid key — i.e. nothing is silently dropped on the
// wire. This is the safety net for the 67-entry hand-ported table.
func TestFabricationsResolveAllEnums(t *testing.T) {
	r := &recorder{}
	s := NewSyntheticUI(r, SyntheticOptions{})
	for _, fab := range ambientFabTable() {
		for range 40 {
			r.commits = nil
			fab.emit(s)
			if len(r.commits) == 0 && fab.event != "" {
				// Every fab commits at least once (some are conditional but always emit).
				t.Errorf("fab %s emitted nothing", fab.event)
			}
			for _, c := range r.commits {
				if bad := wam.UnresolvedEnumFields(c.event, c.payload); len(bad) > 0 {
					t.Errorf("fab %s → event %q unresolved enum fields %v", fab.event, c.event, bad)
				}
			}
		}
	}
}

// TestFabricationsGating checks capability-gated fabs are excluded from the
// ambient table unless their flag is on.
func TestFabricationsGating(t *testing.T) {
	countGated := func(opts SyntheticOptions) int {
		s := NewSyntheticUI(&recorder{}, opts)
		// registerAmbientSpecs ran in the constructor; count is len(ambientSpecs).
		return len(s.ambientSpecs)
	}
	base := countGated(SyntheticOptions{})
	all := countGated(SyntheticOptions{Channels: true, Communities: true, Business: true})
	if all <= base {
		t.Fatalf("enabling gates should add ambient specs: base=%d all=%d", base, all)
	}
}

// TestFabricationsCount sanity-checks the table size.
func TestFabricationsCount(t *testing.T) {
	if n := len(ambientFabTable()); n < 60 {
		t.Fatalf("ambient fab table has %d entries, expected ~67", n)
	}
}
