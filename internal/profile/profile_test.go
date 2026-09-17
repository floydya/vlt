package profile

import (
	"strings"
	"testing"
)

func TestValidateName(t *testing.T) {
	tests := []struct {
		name  string
		value string
		valid bool
	}{
		{name: "single letter", value: "a", valid: true},
		{name: "single digit", value: "7", valid: true},
		{name: "allowed punctuation", value: "team-A_2", valid: true},
		{name: "case is preserved", value: "TeamA", valid: true},
		{name: "empty", value: "", valid: false},
		{name: "starts with hyphen", value: "-team", valid: false},
		{name: "starts with underscore", value: "_team", valid: false},
		{name: "contains space", value: "team a", valid: false},
		{name: "contains dot", value: "team.a", valid: false},
		{name: "contains slash", value: "team/a", valid: false},
		{name: "contains non ASCII letter", value: "tém", valid: false},
		{name: "contains control character", value: "team\n", valid: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateName(tt.value)
			if tt.valid && err != nil {
				t.Fatalf("ValidateName(%q) error = %v", tt.value, err)
			}
			if !tt.valid && err == nil {
				t.Fatalf("ValidateName(%q) error = nil, want validation error", tt.value)
			}
		})
	}
}

func TestValidateAddress(t *testing.T) {
	tests := []struct {
		name  string
		value string
		valid bool
	}{
		{name: "https host", value: "https://vault.example.com", valid: true},
		{name: "http host", value: "http://127.0.0.1:8200", valid: true},
		{name: "optional path", value: "https://vault.example.com/base/path", valid: true},
		{name: "IPv6 host and port", value: "https://[::1]:8200", valid: true},
		{name: "empty", value: "", valid: false},
		{name: "whitespace", value: "   ", valid: false},
		{name: "relative URL", value: "vault.example.com", valid: false},
		{name: "missing host", value: "https:///vault", valid: false},
		{name: "unsupported scheme", value: "ftp://vault.example.com", valid: false},
		{name: "userinfo", value: "https://user:password@vault.example.com", valid: false},
		{name: "query", value: "https://vault.example.com?region=one", valid: false},
		{name: "empty query", value: "https://vault.example.com?", valid: false},
		{name: "fragment", value: "https://vault.example.com/#login", valid: false},
		{name: "malformed URL", value: "https://[::1", valid: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateAddress(tt.value)
			if tt.valid && err != nil {
				t.Fatalf("ValidateAddress(%q) error = %v", tt.value, err)
			}
			if !tt.valid && err == nil {
				t.Fatalf("ValidateAddress(%q) error = nil, want validation error", tt.value)
			}
		})
	}
}

func TestValidateAddressDoesNotExposeMalformedInput(t *testing.T) {
	const canary = "TOKEN-CANARY-DO-NOT-LEAK"
	err := ValidateAddress("https://[::1/" + canary)
	if err == nil {
		t.Fatal("ValidateAddress() error = nil, want malformed URL error")
	}
	if strings.Contains(err.Error(), canary) {
		t.Fatalf("ValidateAddress() error exposed input: %q", err)
	}
}

func TestProfileValidation(t *testing.T) {
	valid := Profile{
		Name:      "team-a",
		Address:   "https://vault.example.com",
		Username:  "example-user",
		AuthPath:  "oidc",
		Namespace: "engineering/team-a",
	}

	tests := []struct {
		name    string
		mutate  func(*Profile)
		wantErr string
	}{
		{name: "valid profile"},
		{name: "empty username", mutate: func(p *Profile) { p.Username = "" }, wantErr: "username"},
		{name: "whitespace username", mutate: func(p *Profile) { p.Username = " \t\n" }, wantErr: "username"},
		{name: "surrounding username whitespace is preserved", mutate: func(p *Profile) { p.Username = " example-user " }},
		{name: "NUL in username", mutate: func(p *Profile) { p.Username = "example\x00user" }, wantErr: "username"},
		{name: "control character in username", mutate: func(p *Profile) { p.Username = "example\nuser" }, wantErr: "username"},
		{name: "empty auth path", mutate: func(p *Profile) { p.AuthPath = "" }, wantErr: "auth_path"},
		{name: "whitespace auth path", mutate: func(p *Profile) { p.AuthPath = " \t" }, wantErr: "auth_path"},
		{name: "nested auth path accepted", mutate: func(p *Profile) { p.AuthPath = "auth/oidc" }},
		{name: "surrounding auth path whitespace is preserved", mutate: func(p *Profile) { p.AuthPath = " oidc " }},
		{name: "NUL in auth path", mutate: func(p *Profile) { p.AuthPath = "oidc\x00path" }, wantErr: "auth_path"},
		{name: "control character in auth path", mutate: func(p *Profile) { p.AuthPath = "oidc\rpath" }, wantErr: "auth_path"},
		{name: "empty namespace is optional", mutate: func(p *Profile) { p.Namespace = "" }},
		{name: "whitespace namespace", mutate: func(p *Profile) { p.Namespace = " \n" }, wantErr: "namespace"},
		{name: "surrounding namespace whitespace is preserved", mutate: func(p *Profile) { p.Namespace = " engineering " }},
		{name: "NUL in namespace", mutate: func(p *Profile) { p.Namespace = "engineering\x00team" }, wantErr: "namespace"},
		{name: "control character in namespace", mutate: func(p *Profile) { p.Namespace = "engineering\tteam" }, wantErr: "namespace"},
		{name: "invalid name", mutate: func(p *Profile) { p.Name = "bad name" }, wantErr: "name"},
		{name: "invalid address", mutate: func(p *Profile) { p.Address = "vault.example.com" }, wantErr: "address"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := valid
			if tt.mutate != nil {
				tt.mutate(&got)
			}

			err := got.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v", err)
				}
				if got != valid && tt.mutate != nil {
					// Validation must not normalize accepted values. The mutated value
					// itself is checked by using a value receiver and comparing after validation.
					before := got
					if err := got.Validate(); err != nil {
						t.Fatalf("second Validate() error = %v", err)
					}
					if got != before {
						t.Fatalf("Validate() changed profile: got %#v, want %#v", got, before)
					}
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() error = nil, want error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate() error = %q, want field %q", err, tt.wantErr)
			}
		})
	}
}
