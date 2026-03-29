// Package security implements the security model for sd.
// REQ-004-001 through REQ-004-031
package security

// MountMode represents the access mode for a host mount.
// REQ-004-004
type MountMode int

const (
	MountReadOnly  MountMode = iota
	MountReadWrite
)

// TokenType identifies the kind of credential being validated.
// REQ-004-012
type TokenType int

const (
	TokenGitHubPAT         TokenType = iota
	TokenGitHubFinegrained
	TokenAnthropicAPI
)

// SecurityWarning represents a non-fatal security observation.
// REQ-004-012
type SecurityWarning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// DomainSource indicates whether a domain is from the built-in
// default list or was added by the user.
// REQ-004-007, REQ-004-008
type DomainSource string

const (
	DomainSourceDefault DomainSource = "default"
	DomainSourceUser    DomainSource = "user"
)

// EgressDomain represents an allowed outbound destination.
// REQ-004-007
type EgressDomain struct {
	Domain string       `json:"domain"`
	Source DomainSource `json:"source"`
}

// MountValidationError describes why a mount path was rejected.
// REQ-004-005
type MountValidationError struct {
	Path     string `json:"path"`
	Category string `json:"category"`
	Reason   string `json:"reason"`
}

func (e *MountValidationError) Error() string {
	return e.Reason
}
