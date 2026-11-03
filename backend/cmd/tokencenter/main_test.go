package main

import (
	"strings"
	"testing"
)

func TestLauncherRejectsMissingConfigurationAndProtectedPort(t *testing.T) {
	t.Setenv("TOKENCENTER_DATABASE_URL", "")
	t.Setenv("TOKENCENTER_ADMIN_TOKEN", "")
	if err := run(); err == nil || !strings.Contains(err.Error(), "TOKENCENTER_DATABASE_URL") {
		t.Fatal("missing database configuration not rejected")
	}
	t.Setenv("TOKENCENTER_DATABASE_URL", "postgres://placeholder@127.0.0.1/tokencenter")
	if err := run(); err == nil || !strings.Contains(err.Error(), "TOKENCENTER_ADMIN_TOKEN") {
		t.Fatal("missing admin token not rejected")
	}
	t.Setenv("TOKENCENTER_ADMIN_TOKEN", "test-only")
	t.Setenv("TOKENCENTER_ADDR", "127.0.0.1:8080")
	if err := run(); err == nil || !strings.Contains(err.Error(), "8086") {
		t.Fatal("protected existing service port not rejected before database access")
	}
}
