//go:build !windows

package main

import "fmt"

func relaunchElevatedRetry(req retryRequest) error {
	return fmt.Errorf("elevated retry is only supported on Windows")
}
