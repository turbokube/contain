package main

import (
	"os"
)

// main only delegates to the root cobra command defined in root.go
func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
