package security

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const (
	SessionCookieName = "__Host-sid"
	CSRFCookieName    = "__Host-csrf"
)

var ErrForbidden = errors.New("request failed security validation")

type Protector struct {
	csrfKey []byte
}

func New(secret []byte) (*Protector, error) {
	if len(secret) < 32 {
		return nil, errors.New("APP_SECRET must contain at least 32 bytes")
	}
	key := sha256.Sum256(append([]byte("patient-dashboard/csrf/v1\x00"), secret...))
	return &Protector{csrfKey: key[:]}, nil
}

// NewSessionID returns the URL-safe, unpadded encoding of 160 random bits,
// following Hyperlith's unguessable-session-ID practice.
func NewSessionID() (string, error) {
	value := make([]byte, 20)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate session ID: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func ValidSessionID(sid string) bool {
	value, err := base64.RawURLEncoding.DecodeString(sid)
	return err == nil && len(value) == 20
}

func SessionID(r *http.Request) (string, bool) {
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil || !ValidSessionID(cookie.Value) {
		return "", false
	}
	return cookie.Value, true
}

func SetSessionCookie(w http.ResponseWriter, sid string) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    sid,
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func (p *Protector) NewCSRFToken(sid string) (string, error) {
	nonceBytes := make([]byte, 8)
	if _, err := rand.Read(nonceBytes); err != nil {
		return "", fmt.Errorf("generate CSRF nonce: %w", err)
	}
	nonce := base64.RawURLEncoding.EncodeToString(nonceBytes)
	mac := hmac.New(sha256.New, p.csrfKey)
	_, _ = mac.Write(csrfMessage(sid, nonce))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)) + "." + nonce, nil
}

func (p *Protector) SetCSRFToken(w http.ResponseWriter, sid string) (string, error) {
	token, err := p.NewCSRFToken(sid)
	if err != nil {
		return "", err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     CSRFCookieName,
		Value:    token,
		Path:     "/",
		Secure:   true,
		HttpOnly: false, // Datastar submits this value as an ephemeral signal.
		SameSite: http.SameSiteLaxMode,
	})
	return token, nil
}

func (p *Protector) ValidateUnsafeRequest(r *http.Request, sid, submitted string) error {
	if !ValidSessionID(sid) || submitted == "" || !sameOrigin(r) {
		return ErrForbidden
	}
	// The cookie is the delivery half of the signed double-submit pattern. Do
	// not require it to equal the submitted value: another tab may have rotated
	// the shared cookie, while this tab's session-bound HMAC remains valid.
	if _, err := r.Cookie(CSRFCookieName); err != nil {
		return ErrForbidden
	}

	parts := strings.Split(submitted, ".")
	if len(parts) != 2 {
		return ErrForbidden
	}
	actual, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return ErrForbidden
	}
	if nonce, err := base64.RawURLEncoding.DecodeString(parts[1]); err != nil || len(nonce) != 8 {
		return ErrForbidden
	}

	mac := hmac.New(sha256.New, p.csrfKey)
	_, _ = mac.Write(csrfMessage(sid, parts[1]))
	if !hmac.Equal(actual, mac.Sum(nil)) {
		return ErrForbidden
	}
	return nil
}

func csrfMessage(sid, nonce string) []byte {
	return fmt.Appendf(nil, "%d!%s%d!%s", len(sid), sid, len(nonce), nonce)
}

func sameOrigin(r *http.Request) bool {
	fetchSite := r.Header.Get("Sec-Fetch-Site")
	if fetchSite != "" && fetchSite != "same-origin" {
		return false
	}

	origin := r.Header.Get("Origin")
	if origin == "" {
		// Hyperlith treats clients without Fetch Metadata as unsafe. A same-origin
		// Fetch Metadata value is sufficient when Origin is omitted.
		return fetchSite == "same-origin"
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host != r.Host {
		return false
	}
	expectedScheme := "http"
	if r.TLS != nil {
		expectedScheme = "https"
	}
	return u.Scheme == expectedScheme
}

func Headers(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		h.Set("Content-Security-Policy", strings.Join([]string{
			"base-uri 'self'",
			"default-src 'none'",
			"form-action 'self'",
			"frame-ancestors 'none'",
			"script-src 'self' 'unsafe-eval' https://cdn.jsdelivr.net",
			"connect-src 'self'",
			"style-src 'self' 'unsafe-inline'",
			"img-src 'self' data:",
			"font-src 'self'",
		}, "; "))
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Permitted-Cross-Domain-Policies", "none")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
}
