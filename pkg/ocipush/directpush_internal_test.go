package ocipush

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func init() {
	// keeps the part retry tests in this package fast
	partRetryBackoff = time.Millisecond
}

func TestStorageErrorBody(t *testing.T) {
	const presigned = "https://acct.r2.example.com/bucket/key?X-Amz-Credential=c&X-Amz-Signature=s3cr3t"
	for _, tc := range []struct {
		name        string
		contentType string
		server      string
		body        string
		want        string
	}{
		{"s3 xml", "application/xml", "",
			`<?xml version="1.0" encoding="UTF-8"?><Error><Code>AccessDenied</Code><Message>Access Denied</Message></Error>`,
			"AccessDenied: Access Denied"},
		{"s3 xml truncated after code", "application/xml", "",
			`<Error><Code>SignatureDoesNotMatch</Code><SignatureProvided>s3cr3t</Signa`,
			"SignatureDoesNotMatch"},
		{"html error page", "text/html", "cloudflare",
			"<html>\n<head><title>502 Bad Gateway</title></head>\n<body><center>cloudflare</center></body></html>",
			`html page "502 Bad Gateway" from cloudflare`},
		{"html without content type", "", "",
			"<!DOCTYPE html><html><body>oops</body></html>",
			"html page"},
		{"plain text echoing the signature", "text/plain", "",
			"bad request for X-Amz-Credential=c&X-Amz-Signature=s3cr3t, signature s3cr3t",
			"bad request for [redacted], signature [redacted]"},
		{"long text", "text/plain", "", strings.Repeat("x ", 300), strings.Repeat("x ", 100)[:200] + "..."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := &http.Response{Header: http.Header{}, Body: io.NopCloser(strings.NewReader(tc.body))}
			if tc.contentType != "" {
				res.Header.Set("Content-Type", tc.contentType)
			}
			if tc.server != "" {
				res.Header.Set("Server", tc.server)
			}
			if got := storageErrorBody(res, presigned); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestStorageOrigin(t *testing.T) {
	if got := storageOrigin("https://acct.r2.example.com/bucket/key?X-Amz-Signature=s3cr3t"); got != "https://acct.r2.example.com" {
		t.Errorf("got %q", got)
	}
	if got := storageOrigin("::not a url"); got != "object storage" {
		t.Errorf("got %q", got)
	}
}

func TestForbiddenNearDeadlineIsExpiry(t *testing.T) {
	issued := time.Now().Add(-1 * time.Hour)
	fresh := &extStartResponse{ExpiresSeconds: 7200, issued: issued, deadline: issued.Add(2 * time.Hour)}
	if err := fresh.forbidden("part 1", "AccessDenied"); !strings.Contains(err.Error(), errExtRefused.Error()) {
		t.Errorf("an hour into two is not expiry: %v", err)
	}
	near := &extStartResponse{ExpiresSeconds: 3700, issued: issued, deadline: issued.Add(3700 * time.Second)}
	if err := near.forbidden("part 1", "AccessDenied"); !strings.Contains(err.Error(), errExtExpired.Error()) {
		t.Errorf("within the margin of the deadline is expiry: %v", err)
	}
	unbounded := &extStartResponse{issued: issued}
	if err := unbounded.forbidden("part 1", "AccessDenied"); !strings.Contains(err.Error(), errExtExpired.Error()) {
		t.Errorf("with no stated deadline a 403 is still treated as expiry: %v", err)
	}
}
