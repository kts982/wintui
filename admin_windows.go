package main

import (
	"sync"

	"golang.org/x/sys/windows"
)

// isElevated reports whether the current process is running with
// administrator (elevated) privileges on Windows.
var isElevated = sync.OnceValue(func() bool {
	t := windows.GetCurrentProcessToken()
	return t.IsElevated()
})
