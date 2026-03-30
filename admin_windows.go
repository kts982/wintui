package main

import "golang.org/x/sys/windows"

// isElevated reports whether the current process is running with
// administrator (elevated) privileges on Windows.
// It is a variable so tests can override it.
var isElevated = func() bool {
	return windows.GetCurrentProcessToken().IsElevated()
}
