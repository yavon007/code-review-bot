package server

import (
	"net/http/httptest"
	"testing"
)

func TestSetSessionCookieUsesSecureAttributes(t *testing.T) {
	res := httptest.NewRecorder()
	setSessionCookie(res, "test-secret", 1, 2)

	cookies := res.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}
	cookie := cookies[0]
	if !cookie.Secure {
		t.Fatal("expected session cookie to be Secure")
	}
	if !cookie.HttpOnly {
		t.Fatal("expected session cookie to be HttpOnly")
	}
}

func TestSessionVersionRoundTrip(t *testing.T) {
	payload := sessionPayload{UserID: 1, Exp: 4102444800, SessionVersion: 7}
	decoded, ok := decodeSession("test-secret", encodeSession("test-secret", payload))
	if !ok {
		t.Fatal("expected session to decode")
	}
	if decoded.SessionVersion != payload.SessionVersion {
		t.Fatalf("expected session version %d, got %d", payload.SessionVersion, decoded.SessionVersion)
	}
}
