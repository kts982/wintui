package main

import (
	"flag"
	"fmt"
	"io"
)

type retryOp string

const (
	retryOpInstall   retryOp = "install"
	retryOpUpgrade   retryOp = "upgrade"
	retryOpUninstall retryOp = "uninstall"
)

type retryRequest struct {
	Op      retryOp
	ID      string
	Name    string
	Source  string
	Version string
}

func (r retryRequest) valid() bool {
	switch r.Op {
	case retryOpInstall, retryOpUpgrade, retryOpUninstall:
		return r.ID != ""
	default:
		return false
	}
}

func parseStartupArgs(args []string) (bool, *retryRequest, error) {
	fs := flag.NewFlagSet("wintui", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	showVersion := fs.Bool("version", false, "")
	shortVersion := fs.Bool("v", false, "")
	retryOpVal := fs.String("retry-op", "", "")
	retryID := fs.String("id", "", "")
	retryName := fs.String("name", "", "")
	retrySource := fs.String("source", "", "")
	retryVersion := fs.String("package-version", "", "")

	if err := fs.Parse(args); err != nil {
		return false, nil, err
	}
	if *showVersion || *shortVersion {
		return true, nil, nil
	}
	if *retryOpVal == "" {
		return false, nil, nil
	}

	req := &retryRequest{
		Op:      retryOp(*retryOpVal),
		ID:      *retryID,
		Name:    *retryName,
		Source:  *retrySource,
		Version: *retryVersion,
	}
	if !req.valid() {
		return false, nil, fmt.Errorf("invalid retry request")
	}
	return false, req, nil
}

func tabForRetry(req retryRequest) int {
	switch req.Op {
	case retryOpInstall:
		return 2
	case retryOpUpgrade:
		return 0
	case retryOpUninstall:
		return 1
	default:
		return 0
	}
}
