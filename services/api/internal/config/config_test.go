package config

import "testing"

// TestValidateRLSEnforcement covers the production guard (INFRA-01/AUTH-25):
// outside development a configured database forces the non-owner fuelgrid_app
// pool, so the API can't silently run RLS-bypassed on the table owner.
func TestValidateRLSEnforcement(t *testing.T) {
	const owner = "postgres://fuelgrid:fuelgrid@db:5432/fuelgrid"
	const app = "postgres://fuelgrid_app:secret@db:5432/fuelgrid"
	https := []string{"https://app.example.com"}

	cases := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{"dev skips all checks", Config{Env: "development", CORSOrigins: []string{"*"}, DatabaseURL: owner}, false},
		{"prod requires app url", Config{Env: "production", CORSOrigins: https, DatabaseURL: owner}, true},
		{"prod rejects app==owner", Config{Env: "production", CORSOrigins: https, DatabaseURL: owner, DatabaseAppURL: owner}, true},
		{"prod ok with distinct app url", Config{Env: "production", CORSOrigins: https, DatabaseURL: owner, DatabaseAppURL: app}, false},
		{"prod no db is exempt", Config{Env: "production", CORSOrigins: https}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.cfg.validate()
			if (err != nil) != c.wantErr {
				t.Fatalf("validate() err=%v, wantErr=%v", err, c.wantErr)
			}
		})
	}
}
