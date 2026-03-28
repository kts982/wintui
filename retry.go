package main

import (
	"encoding/base64"
	"encoding/json"
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
	Items   []retryItem
}

type retryItem struct {
	ID      string `json:"id"`
	Name    string `json:"name,omitempty"`
	Source  string `json:"source,omitempty"`
	Version string `json:"version,omitempty"`
}

func (r retryRequest) valid() bool {
	switch r.Op {
	case retryOpInstall, retryOpUpgrade, retryOpUninstall:
		items := r.items()
		if len(items) == 0 {
			return false
		}
		for _, item := range items {
			if item.ID == "" {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func (r retryRequest) items() []retryItem {
	if len(r.Items) > 0 {
		return append([]retryItem(nil), r.Items...)
	}
	if r.ID == "" {
		return nil
	}
	return []retryItem{{
		ID:      r.ID,
		Name:    r.Name,
		Source:  r.Source,
		Version: r.Version,
	}}
}

func (r retryRequest) isBatch() bool {
	return len(r.items()) > 1
}

func (r retryRequest) startupArgs() ([]string, error) {
	args := []string{"--retry-op", string(r.Op)}
	items := r.items()
	if len(items) > 1 {
		payload, err := encodeRetryItems(items)
		if err != nil {
			return nil, err
		}
		args = append(args, "--retry-batch", payload)
		return args, nil
	}
	if len(items) == 1 {
		item := items[0]
		args = append(args, "--id", item.ID)
		if item.Name != "" {
			args = append(args, "--name", item.Name)
		}
		if item.Source != "" {
			args = append(args, "--source", item.Source)
		}
		if item.Version != "" {
			args = append(args, "--package-version", item.Version)
		}
	}
	return args, nil
}

func newRetryRequest(op retryOp, items []retryItem) *retryRequest {
	if len(items) == 0 {
		return nil
	}
	if len(items) == 1 {
		item := items[0]
		return &retryRequest{
			Op:      op,
			ID:      item.ID,
			Name:    item.Name,
			Source:  item.Source,
			Version: item.Version,
		}
	}
	return &retryRequest{
		Op:    op,
		Items: append([]retryItem(nil), items...),
	}
}

func encodeRetryItems(items []retryItem) (string, error) {
	data, err := json.Marshal(items)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func decodeRetryItems(payload string) ([]retryItem, error) {
	data, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return nil, err
	}
	var items []retryItem
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func retryHintText(req *retryRequest) string {
	target := "the failed package"
	if req != nil && req.isBatch() {
		target = "the failed packages"
	}
	return fmt.Sprintf("Press Ctrl+e to relaunch elevated and retry %s.", target)
}

func retryWarningText(req *retryRequest) string {
	if req == nil {
		return ""
	}
	switch req.Op {
	case retryOpInstall:
		return "Retrying elevated may install machine-wide instead of per-user for some packages."
	case retryOpUpgrade:
		return "Retrying elevated may change installer behavior for some packages."
	case retryOpUninstall:
		return "Retrying elevated may help remove packages blocked by permissions or services."
	default:
		return ""
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
	retryBatch := fs.String("retry-batch", "", "")

	if err := fs.Parse(args); err != nil {
		return false, nil, err
	}
	if *showVersion || *shortVersion {
		return true, nil, nil
	}
	if *retryOpVal == "" {
		return false, nil, nil
	}

	req := &retryRequest{Op: retryOp(*retryOpVal)}
	if *retryBatch != "" {
		items, err := decodeRetryItems(*retryBatch)
		if err != nil {
			return false, nil, fmt.Errorf("invalid retry batch: %w", err)
		}
		req.Items = items
	} else {
		req.ID = *retryID
		req.Name = *retryName
		req.Source = *retrySource
		req.Version = *retryVersion
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
