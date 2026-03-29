package main

import (
	"os"

	"github.com/gberns/sd/internal/cmd"
)

func main() {
	os.Exit(cmd.Execute())
}
