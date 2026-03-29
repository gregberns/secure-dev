// Package modules provides embedded built-in provisioning module YAML files.
// REQ-006-014: Built-in modules are embedded in the binary via //go:embed.
package modules

import "embed"

// ModuleFS provides access to the embedded module YAML files.
// Each file is a standalone module definition following the schema in REQ-006-003.
//
//go:embed *.yaml
var ModuleFS embed.FS
