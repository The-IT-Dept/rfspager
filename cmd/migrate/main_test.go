package main

import (
	"context"
	"strings"
	"testing"
)

func TestRunRejectsMissingDatabaseURL(t *testing.T) {
	err := run(context.Background(), "")
	if err == nil {
		t.Fatal("missing database URL unexpectedly succeeded")
	}
	if !strings.Contains(err.Error(), "database URL") {
		t.Fatalf("missing database URL error = %q", err)
	}
}
