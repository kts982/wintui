package main

import (
	"encoding/base64"
	"encoding/json"
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

func tabForRetry(_ retryRequest) int {
	return 0 // All operations now on Packages (workspace) tab
}
