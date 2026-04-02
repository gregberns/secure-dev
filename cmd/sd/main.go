// Package main is the entry point for the sd CLI.
// REQ-002-001: CLI binary named sd, entry point at cmd/sd/main.go
package main

import (
	"sd/internal/cmd"

	// Register backends via init() functions.
	_ "sd/internal/backend/avf"
	_ "sd/internal/backend/docker"
	_ "sd/internal/backend/incus"
	_ "sd/internal/backend/lima"
)

func main() {
	_ = cmd.Execute()
}
