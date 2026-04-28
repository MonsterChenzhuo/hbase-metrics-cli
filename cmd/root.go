package cmd

import "fmt"

var version = "dev"

// Execute is the package entry point invoked by main.go.
func Execute() int {
	fmt.Println("hbase-metrics-cli", version)
	return 0
}
