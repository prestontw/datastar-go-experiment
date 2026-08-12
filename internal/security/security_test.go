package security

import (
	"crypto/tls"
	"net/http/httptest"
	"testing"
)

func TestCSRFTokenIsBoundToSessionAndCookie(t *testing.T) {
	protector, err := New([]byte("a-test-secret-that-is-at-least-thirty-two-bytes"))
	if err != nil {
		t.Fatal(err)
	}
	sid, err := NewSessionID()
	if err != nil {
		t.Fatal(err)
	}

	response := httptest.NewRecorder()
	token, err := protector.SetCSRFToken(response, sid)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("POST", "https://practice.test/commands", nil)
	request.Host = "practice.test"
	request.TLS = &tls.ConnectionState{}
	request.Header.Set("Origin", "https://practice.test")
	request.AddCookie(response.Result().Cookies()[0])

	if err := protector.ValidateUnsafeRequest(request, sid, token); err != nil {
		t.Fatalf("ValidateUnsafeRequest() error = %v", err)
	}
	if err := protector.ValidateUnsafeRequest(request, sid, token+"x"); err == nil {
		t.Fatal("ValidateUnsafeRequest() accepted a modified token")
	}
}

func TestCSRFAcceptsSignedTokenAfterAnotherTabRotatesCookie(t *testing.T) {
	protector, _ := New([]byte("a-test-secret-that-is-at-least-thirty-two-bytes"))
	sid, _ := NewSessionID()
	firstResponse := httptest.NewRecorder()
	firstToken, _ := protector.SetCSRFToken(firstResponse, sid)
	secondResponse := httptest.NewRecorder()
	_, _ = protector.SetCSRFToken(secondResponse, sid)

	request := httptest.NewRequest("POST", "https://practice.test/commands", nil)
	request.Host = "practice.test"
	request.TLS = &tls.ConnectionState{}
	request.Header.Set("Origin", "https://practice.test")
	request.AddCookie(secondResponse.Result().Cookies()[0])

	if err := protector.ValidateUnsafeRequest(request, sid, firstToken); err != nil {
		t.Fatalf("a second tab invalidated a signed token: %v", err)
	}
}

func TestCSRFRejectsCrossSiteRequest(t *testing.T) {
	protector, _ := New([]byte("a-test-secret-that-is-at-least-thirty-two-bytes"))
	sid, _ := NewSessionID()
	response := httptest.NewRecorder()
	token, _ := protector.SetCSRFToken(response, sid)
	request := httptest.NewRequest("POST", "https://practice.test/commands", nil)
	request.Host = "practice.test"
	request.TLS = &tls.ConnectionState{}
	request.Header.Set("Origin", "https://attacker.test")
	request.Header.Set("Sec-Fetch-Site", "cross-site")
	request.AddCookie(response.Result().Cookies()[0])

	if err := protector.ValidateUnsafeRequest(request, sid, token); err == nil {
		t.Fatal("ValidateUnsafeRequest() accepted a cross-site request")
	}
}

func TestSessionIDsAre160BitURLSafeValues(t *testing.T) {
	sid, err := NewSessionID()
	if err != nil {
		t.Fatal(err)
	}
	if !ValidSessionID(sid) {
		t.Fatalf("generated session ID %q is invalid", sid)
	}
	if ValidSessionID("guessable") {
		t.Fatal("ValidSessionID() accepted a short value")
	}
}
