package main

import (
	"strings"
	"testing"
)

func TestConfirmed(t *testing.T) {
	tests := []struct {
		name   string
		env    string
		args   []string
		want   bool
		setEnv bool
	}{
		{name: "no confirmation", args: []string{"down-all"}, want: false},
		{name: "env confirm 1", env: "1", setEnv: true, args: []string{"down-all"}, want: true},
		{name: "env confirm other value", env: "true", setEnv: true, args: []string{"down-all"}, want: false},
		{name: "double-dash yes flag", args: []string{"down-all", "--yes"}, want: true},
		{name: "single-dash yes flag", args: []string{"--yes", "force", "3"}, want: true},
		{name: "unrelated flag", args: []string{"down-all", "--force"}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setEnv {
				t.Setenv("MIGRATE_CONFIRM", tt.env)
			} else {
				t.Setenv("MIGRATE_CONFIRM", "")
			}
			if got := confirmed(tt.args); got != tt.want {
				t.Fatalf("confirmed(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}

func TestGuardRefusesWithoutConfirmation(t *testing.T) {
	t.Setenv("MIGRATE_CONFIRM", "")
	err := guard("down-all", []string{"down-all"})
	if err == nil {
		t.Fatal("guard should refuse without confirmation")
	}
	if !strings.Contains(err.Error(), "MIGRATE_CONFIRM=1") || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("refusal message should mention both opt-in mechanisms, got: %v", err)
	}
	if !strings.Contains(err.Error(), "down-all") {
		t.Fatalf("refusal message should name the command, got: %v", err)
	}
}

func TestGuardAllowsWithEnv(t *testing.T) {
	t.Setenv("MIGRATE_CONFIRM", "1")
	if err := guard("force", []string{"force", "5"}); err != nil {
		t.Fatalf("guard should allow with MIGRATE_CONFIRM=1, got: %v", err)
	}
}

func TestGuardAllowsWithFlag(t *testing.T) {
	t.Setenv("MIGRATE_CONFIRM", "")
	if err := guard("force", []string{"--yes", "force", "5"}); err != nil {
		t.Fatalf("guard should allow with --yes, got: %v", err)
	}
}
