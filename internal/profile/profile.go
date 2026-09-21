// Package profile defines validated Vault profile metadata.
package profile

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode"
)

var (
	namePattern        = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)
	numericNamePattern = regexp.MustCompile(`^[0-9]+$`)
	colorPattern       = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)
)

// Profile contains the non-secret metadata needed to use a Vault profile.
type Profile struct {
	Name          string `json:"name"`
	Address       string `json:"address"`
	Username      string `json:"username"`
	AuthPath      string `json:"auth_path"`
	Namespace     string `json:"namespace"`
	AllowInsecure bool   `json:"allow_insecure"`
	Color         string `json:"color,omitempty"`
}

// ValidateName checks the stable, case-sensitive profile identifier.
func ValidateName(name string) error {
	if !namePattern.MatchString(name) {
		return fmt.Errorf("name must match %s", namePattern.String())
	}
	if numericNamePattern.MatchString(name) {
		return fmt.Errorf("name must not consist only of digits")
	}
	return nil
}

// ValidateAddress checks that address is an absolute HTTP(S) Vault URL without
// credentials, a query, or a fragment.
func ValidateAddress(address string) error {
	parsed, err := url.Parse(address)
	if err != nil {
		return fmt.Errorf("address must be a valid absolute URL")
	}
	if !strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https") {
		return fmt.Errorf("address scheme must be http or https")
	}
	if !parsed.IsAbs() || parsed.Host == "" || parsed.Hostname() == "" {
		return fmt.Errorf("address must be absolute and include a host")
	}
	if parsed.User != nil {
		return fmt.Errorf("address must not include userinfo")
	}
	if parsed.RawQuery != "" || parsed.ForceQuery {
		return fmt.Errorf("address must not include a query")
	}
	if parsed.Fragment != "" || strings.Contains(address, "#") {
		return fmt.Errorf("address must not include a fragment")
	}
	return nil
}

func ValidateColor(color string) error {
	if color != "" && !colorPattern.MatchString(color) {
		return fmt.Errorf("color must be #RRGGBB")
	}
	return nil
}

// Validate verifies profile metadata without modifying accepted values.
func (p Profile) Validate() error {
	if err := ValidateName(p.Name); err != nil {
		return fmt.Errorf("name: %w", err)
	}
	if err := ValidateAddress(p.Address); err != nil {
		return fmt.Errorf("address: %w", err)
	}
	parsed, _ := url.Parse(p.Address)
	if strings.EqualFold(parsed.Scheme, "http") && !p.AllowInsecure {
		return fmt.Errorf("address: insecure HTTP requires --allow-insecure")
	}
	if strings.TrimSpace(p.Username) == "" {
		return fmt.Errorf("username must not be empty or whitespace-only")
	}
	if containsControl(p.Username) {
		return fmt.Errorf("username must not contain control characters")
	}
	if strings.TrimSpace(p.AuthPath) == "" {
		return fmt.Errorf("auth_path must not be empty or whitespace-only")
	}
	if containsControl(p.AuthPath) {
		return fmt.Errorf("auth_path must not contain control characters")
	}
	if p.Namespace != "" && strings.TrimSpace(p.Namespace) == "" {
		return fmt.Errorf("namespace must be empty or contain a non-whitespace character")
	}
	if containsControl(p.Namespace) {
		return fmt.Errorf("namespace must not contain control characters")
	}
	if err := ValidateColor(p.Color); err != nil {
		return err
	}
	return nil
}

func containsControl(value string) bool {
	return strings.IndexFunc(value, unicode.IsControl) >= 0
}
