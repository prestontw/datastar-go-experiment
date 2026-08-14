package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/preston/go-datastar-patient-dashboard/internal/security"
)

// TestBrowserlessHTTPProtocol drives the application through a real TLS/HTTP2
// loopback connection. It complements direct handler tests and Playwright: it
// checks browser-shaped cookies, CSRF, SSE, and command requests without a DOM
// or browser process.
func TestBrowserlessHTTPProtocol(t *testing.T) {
	application, repository := newTestServer(t)
	server := httptest.NewUnstartedServer(application.Handler())
	server.EnableHTTP2 = true
	server.StartTLS()
	t.Cleanup(server.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := server.Client()
	client.Jar = jar

	shellResponse, err := client.Get(server.URL + "/patients?status=open")
	if err != nil {
		t.Fatal(err)
	}
	shell := readResponse(t, shellResponse)
	if shellResponse.ProtoMajor != 2 {
		t.Fatalf("shell protocol = %q, want HTTP/2", shellResponse.Proto)
	}
	if !strings.Contains(shell, `<main id="morph"`) {
		t.Fatal("shell response does not contain the morph target")
	}

	origin, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	csrf := cookieValue(jar.Cookies(origin), security.CSRFCookieName)
	if csrf == "" || cookieValue(jar.Cookies(origin), security.SessionCookieName) == "" {
		t.Fatalf("client cookie jar did not retain session and CSRF cookies: %#v", jar.Cookies(origin))
	}

	streamContext, cancelStream := context.WithTimeout(context.Background(), 5*time.Second)
	pageStreamID := "019fbd32-0800-7000-8000-000000000003"
	streamRequest := browserRequest(t, streamContext, server.URL+"/patients?status=open", server.URL, map[string]any{
		"csrf":               csrf,
		"tabId":              "019fbd32-0800-7000-8000-000000000002",
		"pageStreamId":       pageStreamID,
		"pageStreamRevision": 1,
	})
	streamResponse, err := client.Do(streamRequest)
	if err != nil {
		cancelStream()
		t.Fatal(err)
	}
	defer cancelStream()
	defer streamResponse.Body.Close()
	if streamResponse.ProtoMajor != 2 {
		t.Fatalf("stream protocol = %q, want HTTP/2", streamResponse.Proto)
	}
	if got := streamResponse.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("stream Content-Type = %q", got)
	}
	if event := readSSEEvent(t, streamResponse.Body); !strings.Contains(event, `<main id="morph"`) {
		t.Fatalf("initial SSE event does not contain dashboard main: %s", event)
	}
	cancelStream()
	_ = streamResponse.Body.Close()

	staleRequest := browserRequest(t, context.Background(), server.URL+"/patients?status=open", server.URL, map[string]any{
		"csrf":               csrf,
		"pageStreamId":       pageStreamID,
		"pageStreamRevision": 0,
	})
	staleResponse, err := client.Do(staleRequest)
	if err != nil {
		t.Fatal(err)
	}
	_ = readResponse(t, staleResponse)
	if staleResponse.StatusCode != http.StatusNoContent {
		t.Fatalf("stale page stream status = %s, want 204 No Content", staleResponse.Status)
	}

	commandRequest := browserRequest(t, context.Background(), server.URL+"/commands?command=create-patient", server.URL, map[string]any{
		"csrf":            csrf,
		"patientName":     "Protocol Patient",
		"patientDob":      "1991-02-03",
		"patientPronouns": "they/them",
		"patientCareTeam": "HTTP Client",
	})
	commandResponse, err := client.Do(commandRequest)
	if err != nil {
		t.Fatal(err)
	}
	commandBody := readResponse(t, commandResponse)
	if commandResponse.StatusCode != http.StatusOK {
		t.Fatalf("command status = %d, body = %s", commandResponse.StatusCode, commandBody)
	}
	if !strings.Contains(commandBody, "datastar-patch-signals") {
		t.Fatalf("command response is not a Datastar signal patch: %s", commandBody)
	}
	if repository.createdPatient.Name != "Protocol Patient" {
		t.Fatalf("created patient = %#v", repository.createdPatient)
	}
}

func browserRequest(t *testing.T, ctx context.Context, target, origin string, signals map[string]any) *http.Request {
	t.Helper()
	body, err := json.Marshal(signals)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Accept", "text/event-stream")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Datastar-Request", "true")
	request.Header.Set("Origin", origin)
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	return request
}

func readSSEEvent(t *testing.T, body io.Reader) string {
	t.Helper()
	reader := bufio.NewReader(body)
	var event strings.Builder
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read SSE event: %v; partial event: %s", err, event.String())
		}
		event.WriteString(line)
		if line == "\n" || line == "\r\n" {
			return event.String()
		}
	}
}

func readResponse(t *testing.T, response *http.Response) string {
	t.Helper()
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func cookieValue(cookies []*http.Cookie, name string) string {
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie.Value
		}
	}
	return ""
}
