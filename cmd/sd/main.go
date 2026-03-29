// Package main is the entry point for the sd CLI.
// REQ-002-001: CLI binary named sd, entry point at cmd/sd/main.go
package main

import "sd/internal/cmd"

func main() {
	_ = cmd.Execute()
}
