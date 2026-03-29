package security

import (
	"fmt"
	"strings"
)

// DefaultEgressAllowlist is the built-in set of allowed egress domains.
// REQ-004-007: Default Egress Allowlist
var DefaultEgressAllowlist = []string{
	"api.anthropic.com",
	"github.com",
	"*.githubusercontent.com",
	"archive.ubuntu.com",
	"security.ubuntu.com",
	"deb.debian.org",
	"registry.npmjs.org",
	"pypi.org",
	"files.pythonhosted.org",
	"proxy.golang.org",
	"sum.golang.org",
}

// DomainMatches checks if a hostname matches a domain pattern.
// Wildcard patterns (e.g., "*.example.com") match exactly one subdomain level.
// "*.example.com" matches "foo.example.com" but NOT "bar.foo.example.com".
// REQ-004-007: Wildcard matching
func DomainMatches(pattern, hostname string) bool {
	pattern = strings.ToLower(strings.TrimSpace(pattern))
	hostname = strings.ToLower(strings.TrimSpace(hostname))

	// Exact match
	if pattern == hostname {
		return true
	}

	// Wildcard matching
	if strings.HasPrefix(pattern, "*.") {
		suffix := pattern[1:] // ".example.com"
		// Hostname must end with the suffix
		if !strings.HasSuffix(hostname, suffix) {
			return false
		}
		// The prefix before the suffix must be a single label (no dots)
		prefix := hostname[:len(hostname)-len(suffix)]
		if prefix == "" {
			return false
		}
		// Must contain exactly one label (no dots in prefix)
		return !strings.Contains(prefix, ".")
	}

	return false
}

// BuildEgressList merges the default allowlist with user-added domains.
// REQ-004-008: User additions are merged with (not replacing) the default allowlist.
func BuildEgressList(userDomains []string) []EgressDomain {
	seen := make(map[string]bool)
	var result []EgressDomain

	// Add defaults first
	for _, d := range DefaultEgressAllowlist {
		key := strings.ToLower(d)
		if !seen[key] {
			seen[key] = true
			result = append(result, EgressDomain{
				Domain: d,
				Source: DomainSourceDefault,
			})
		}
	}

	// Add user domains
	for _, d := range userDomains {
		key := strings.ToLower(strings.TrimSpace(d))
		if key == "" {
			continue
		}
		if !seen[key] {
			seen[key] = true
			result = append(result, EgressDomain{
				Domain: d,
				Source: DomainSourceUser,
			})
		}
	}

	return result
}

// IsEgressAllowed checks if a hostname is allowed by the egress allowlist.
// REQ-004-006: Default-deny outbound network policy
func IsEgressAllowed(hostname string, allowlist []EgressDomain) bool {
	for _, d := range allowlist {
		if DomainMatches(d.Domain, hostname) {
			return true
		}
	}
	return false
}

// ValidateEgressDomain validates that a domain pattern is well-formed.
func ValidateEgressDomain(domain string) error {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return fmt.Errorf("domain cannot be empty")
	}
	if strings.HasPrefix(domain, ".") {
		return fmt.Errorf("domain %q cannot start with a dot", domain)
	}
	if strings.Contains(domain, "..") {
		return fmt.Errorf("domain %q contains consecutive dots", domain)
	}
	if strings.HasPrefix(domain, "*.") && strings.Count(domain, ".") < 2 {
		return fmt.Errorf("wildcard domain %q must have at least two labels (e.g., *.example.com)", domain)
	}
	return nil
}
