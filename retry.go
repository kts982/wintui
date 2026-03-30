package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

type retryOp string

const (
	retryOpInstall   retryOp = "install"
	retryOpUpgrade   retryOp = "upgrade"
	retryOpUninstall retryOp = "uninstall"
)

type retryItem struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Source  string `json:"source"`
	Version string `json:"version"`
}

type elevationRetryInfo struct {
	req  *retryRequest
	hard bool
}

type retryRequest struct {
	Op retryOp `json:"op"`
	// Single package
	ID      string `json:"id,omitempty"`
	Name    string `json:"name,omitempty"`
	Source  string `json:"source,omitempty"`
	Version string `json:"version,omitempty"`
	// Batch
	Items []retryItem `json:"items,omitempty"`
}

func (r retryRequest) isBatch() bool {
	return len(r.Items) > 0
}

func (r retryRequest) items() []retryItem {
	if r.isBatch() {
		return r.Items
	}
	if r.ID != "" {
		return []retryItem{{ID: r.ID, Name: r.Name, Source: r.Source, Version: r.Version}}
	}
	return nil
}

func (r retryRequest) valid() bool {
	if r.isBatch() {
		return true
	}
	return r.ID != "" && r.Op != ""
}

func (r retryRequest) startupArgs() ([]string, error) {
	args := []string{"--retry-op", string(r.Op)}
	if r.isBatch() {
		batch, err := encodeRetryItems(r.Items)
		if err != nil {
			return nil, err
		}
		args = append(args, "--retry-batch", batch)
	} else {
		args = append(args, "--id", r.ID)
		if r.Name != "" {
			args = append(args, "--name", r.Name)
		}
		if r.Source != "" {
			args = append(args, "--source", r.Source)
		}
		if r.Version != "" {
			args = append(args, "--package-version", r.Version)
		}
	}
	return args, nil
}

func newRetryItem(pkg Package, version string) retryItem {
	return retryItem{
		ID:      pkg.ID,
		Name:    pkg.Name,
		Source:  pkg.Source,
		Version: version,
	}
}

func newRetryRequestForPackage(op retryOp, pkg Package, version string) *retryRequest {
	return newRetryRequestFromItems(op, []retryItem{newRetryItem(pkg, version)})
}

func newRetryRequestFromItems(op retryOp, items []retryItem) *retryRequest {
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
	return fmt.Sprintf("Press Ctrl+e to retry %s with elevated privileges.", target)
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
