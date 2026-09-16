package main

import (
    "net/http/httptest"
    "testing"
)

func TestHealth(t *testing.T) {
    rr := httptest.NewRecorder()
    req := httptest.NewRequest("GET", "/health", nil)
    (&App{}).health(rr, req)
    if rr.Code != 200 { t.Fatalf("expected 200, got %d", rr.Code) }
}
