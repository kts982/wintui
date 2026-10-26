//go:build !faultinject

package main

// Release builds inject no helper faults; the hooks compile to nothing. See
// faultinject_on.go for the test build (`go build -tags faultinject`).

func helperFaultArgs() []string { return nil }

func helperFault(string) bool { return false }

func helperFaultLog(string, ...any) {}
