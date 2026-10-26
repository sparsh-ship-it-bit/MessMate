package main

import (
    "net/http/httptest"
    "strings"
    "testing"
)

func TestHealth(t *testing.T) {
    rr := httptest.NewRecorder()
    req := httptest.NewRequest("GET", "/health", nil)
    (&App{}).health(rr, req)
    if rr.Code != 200 { t.Fatalf("expected 200, got %d", rr.Code) }
}


func TestValidEmailAddress(t *testing.T) {
    tests := []struct {
        name  string
        value string
        want  bool
    }{
        {"ordinary mailbox", "owner@example.com", true},
        {"trim surrounding whitespace", " owner@example.com ", true},
        {"missing domain", "owner@", false},
        {"display name is not a bare mailbox", "Owner <owner@example.com>", false},
        {"reject header newline", "owner@example.com\r\nBcc: attacker@example.com", false},
        {"reject missing at sign", "owner.example.com", false},
        {"reject empty", "", false},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            if got := validEmailAddress(tt.value); got != tt.want {
                t.Fatalf("validEmailAddress(%q) = %v, want %v", tt.value, got, tt.want)
            }
        })
    }
}

func TestForgotPasswordRejectsInvalidEmailBeforeDatabaseAccess(t *testing.T) {
    req := httptest.NewRequest("POST", "/api/v1/auth/forgot-password", strings.NewReader(
        `{"email":"owner@example.com
Bcc: attacker@example.com"}`,
    ))
    rr := httptest.NewRecorder()
    requestPasswordReset(nil, rr, req)
    if rr.Code != 400 {
        t.Fatalf("expected 400 for invalid email, got %d: %s", rr.Code, rr.Body.String())
    }
}

func TestResetPasswordRequiresTokenAndLongPassword(t *testing.T) {
    tests := []struct {
        name string
        body string
    }{
        {"missing token", `{"password":"long-enough-password"}`},
        {"short password", `{"token":"token","password":"short"}`},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            req := httptest.NewRequest("POST", "/api/v1/auth/reset-password", strings.NewReader(tt.body))
            rr := httptest.NewRecorder()
            completePasswordReset(nil, rr, req)
            if rr.Code != 400 {
                t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
            }
        })
    }
}
