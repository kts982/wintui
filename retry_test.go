package main

import (
	"reflect"
	"testing"
)

func TestRetryRequest_JSON(t *testing.T) {
	items := []retryItem{
		{ID: "id1", Name: "name1", Source: "src1", Version: "v1"},
		{ID: "id2", Name: "name2", Source: "src2", Version: "v2"},
	}
	req := retryRequest{
		Op:    retryOpUpgrade,
		Items: items,
	}

	encoded, err := encodeRetryItems(req.Items)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}

	decoded, err := decodeRetryItems(encoded)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if !reflect.DeepEqual(req.Items, decoded) {
		t.Errorf("expected %v, got %v", req.Items, decoded)
	}
}

func TestTabForRetry(t *testing.T) {
	tests := []struct {
		op   retryOp
		want int
	}{
		{retryOpUpgrade, 0},
		{retryOpUninstall, 1},
		{retryOpInstall, 2},
	}

	for _, tt := range tests {
		req := retryRequest{Op: tt.op, ID: "pkg"}
		if got := tabForRetry(req); got != tt.want {
			t.Errorf("tabForRetry(%v) = %v, want %v", tt.op, got, tt.want)
		}
	}
}
