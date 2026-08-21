package server

import "testing"

func TestAttendantOnlyAccess(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		roles         []string
		isSystemAdmin bool
		want          bool
	}{
		{name: "attendant only", roles: []string{"attendant"}, want: true},
		{name: "system admin with attendant role", roles: []string{"attendant"}, isSystemAdmin: true},
		{name: "attendant plus supervisor", roles: []string{"attendant", "station_manager"}},
		{name: "supervisor only", roles: []string{"station_manager"}},
		{name: "no roles", roles: nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := attendantOnlyAccess(tc.roles, tc.isSystemAdmin); got != tc.want {
				t.Fatalf(
					"attendantOnlyAccess(%v, %v) = %v, want %v",
					tc.roles,
					tc.isSystemAdmin,
					got,
					tc.want,
				)
			}
		})
	}
}
