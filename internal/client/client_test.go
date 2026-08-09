package client

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const syntheticAPIKey = "test-api-key-sentinel-82df5"

func TestParseBaseURLNormalizesAndPreservesSubpath(t *testing.T) {
	t.Parallel()

	parsed, err := ParseBaseURL("  https://example.test/chaptarr/%73ub/  ")
	if err != nil {
		t.Fatalf("ParseBaseURL returned an error: %v", err)
	}
	if got, want := parsed.String(), "https://example.test/chaptarr/%73ub"; got != want {
		t.Fatalf("normalized URL = %q, want %q", got, want)
	}
}

func TestParseBaseURLRejectsUnsafeValuesWithoutEchoingThem(t *testing.T) {
	t.Parallel()

	tests := []string{
		"",
		"ftp://example.test/chaptarr",
		"https:///chaptarr",
		"https://user:credential-sentinel@example.test/chaptarr",
		"https://example.test/chaptarr?api_key=credential-sentinel",
		"https://example.test/chaptarr#credential-sentinel",
		"https://example.test/chaptarr/../admin",
		"https://example.test/chaptarr/%2e%2e/admin",
	}

	for _, raw := range tests {
		t.Run(raw, func(t *testing.T) {
			_, err := ParseBaseURL(raw)
			if err == nil {
				t.Fatal("expected URL to be rejected")
			}
			if strings.Contains(err.Error(), "credential-sentinel") {
				t.Fatalf("validation error leaked input credential: %v", err)
			}
		})
	}
}

func TestClientUsesHeaderAuthenticationAndPreservesProxySubpath(t *testing.T) {
	t.Parallel()

	var observedQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if got, want := req.URL.Path, "/proxy/api/v1/system/status"; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		if got, want := req.Header.Get("X-Api-Key"), syntheticAPIKey; got != want {
			t.Errorf("X-Api-Key = %q, want synthetic key", got)
		}
		if got, want := req.UserAgent(), "terraform-provider-chaptarr/test"; got != want {
			t.Errorf("User-Agent = %q, want %q", got, want)
		}
		observedQuery = req.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"appName":"Chaptarr","version":"0.9.925"}`)
	}))
	defer server.Close()

	client := newTestClient(t, server.URL+"/proxy/")
	status, err := client.SystemStatus(context.Background())
	if err != nil {
		t.Fatalf("SystemStatus returned an error: %v", err)
	}
	if status.ApplicationName != "Chaptarr" || status.Version != "0.9.925" {
		t.Fatalf("unexpected status: %#v", status)
	}
	if observedQuery != "" {
		t.Fatalf("API key must not appear in a query; observed %q", observedQuery)
	}
}

func TestClientRejectsCrossOriginNetworkPathsTraversalAndRedirects(t *testing.T) {
	t.Parallel()

	var foreignRequests atomic.Int32
	foreign := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		foreignRequests.Add(1)
	}))
	defer foreign.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Redirect(w, &http.Request{}, foreign.URL+"/capture", http.StatusFound)
	}))
	defer redirector.Close()

	client := newTestClient(t, redirector.URL+"/proxy")
	for _, requestPath := range []string{
		foreign.URL + "/capture",
		"//foreign.example/capture",
		"/api/v1/../admin",
		"/api/v1/%2e%2e/admin",
		"/api/v1/status?api_key=credential-in-query",
		"/api/v1/status?safe=" + syntheticAPIKey,
		"/api/v1/status?api_key=" + syntheticAPIKey + ";ignored=x",
	} {
		if _, err := client.Do(context.Background(), http.MethodGet, requestPath, nil); err == nil {
			t.Fatalf("expected %q to be rejected", requestPath)
		}
	}

	if _, err := client.Do(context.Background(), http.MethodGet, "/redirect", nil); err == nil {
		t.Fatal("expected cross-origin redirect to be rejected")
	}
	if got := foreignRequests.Load(); got != 0 {
		t.Fatalf("cross-origin target received %d requests; API key could have leaked", got)
	}
}

func TestClientRejectsSameOriginRedirectWithMalformedSensitiveQuery(t *testing.T) {
	t.Parallel()

	var targetRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/start":
			w.Header().Set("Location", "/target?api_key="+syntheticAPIKey+";ignored=x")
			w.WriteHeader(http.StatusFound)
		case "/target":
			targetRequests.Add(1)
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	if _, err := client.Do(context.Background(), http.MethodGet, "/start", nil); err == nil {
		t.Fatal("expected malformed sensitive redirect query to be rejected")
	}
	if got := targetRequests.Load(); got != 0 {
		t.Fatalf("malformed sensitive redirect target received %d requests", got)
	}
}

func TestClientAllowsSameOriginAbsoluteURLWithinBasePath(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/proxy/api/v1/system/status" {
			t.Fatalf("unexpected path %q", req.URL.Path)
		}
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()

	client := newTestClient(t, server.URL+"/proxy")
	if _, err := client.Do(context.Background(), http.MethodGet, server.URL+"/proxy/api/v1/system/status?mode=safe", nil); err != nil {
		t.Fatalf("same-origin absolute URL was rejected: %v", err)
	}
	if _, err := client.Do(context.Background(), http.MethodGet, server.URL+"/outside", nil); err == nil {
		t.Fatal("absolute URL outside the configured base path was accepted")
	}
}

func TestClientRetriesSafeMethodsAndBoundsRetryAfter(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		current := attempts.Add(1)
		if current < 3 {
			w.Header().Set("Retry-After", "3600")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	client.retryDelay = time.Millisecond
	client.maxRetryAfter = 3 * time.Millisecond
	started := time.Now()
	if _, err := client.Do(context.Background(), http.MethodGet, "/api/v1/system/status", nil); err != nil {
		t.Fatalf("GET after retries returned an error: %v", err)
	}
	if got := attempts.Load(); got != 3 {
		t.Fatalf("attempts = %d, want 3", got)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("bounded Retry-After took too long: %s", elapsed)
	}
}

func TestClientDoesNotRetryMutatingMethods(t *testing.T) {
	t.Parallel()

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			var attempts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				attempts.Add(1)
				w.WriteHeader(http.StatusServiceUnavailable)
			}))
			defer server.Close()

			client := newTestClient(t, server.URL)
			if _, err := client.Do(context.Background(), method, "/api/v1/action", []byte(`{}`)); err == nil {
				t.Fatal("expected an API error")
			}
			if got := attempts.Load(); got != 1 {
				t.Fatalf("attempts = %d, want 1", got)
			}
		})
	}
}

func TestClientDoesNotFollowRedirectsForMutatingMethods(t *testing.T) {
	t.Parallel()

	var targetRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/start":
			http.Redirect(w, req, "/target", http.StatusTemporaryRedirect)
		case "/target":
			targetRequests.Add(1)
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	if _, err := client.Do(context.Background(), http.MethodPost, "/start", []byte(`{}`)); err == nil {
		t.Fatal("expected mutating redirect to be rejected")
	}
	if got := targetRequests.Load(); got != 0 {
		t.Fatalf("mutating redirect target received %d requests", got)
	}
}

func TestClientRetriesTransientNetworkErrorsOnlyForSafeMethods(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		method       string
		wantAttempts int32
		wantError    bool
	}{
		{method: http.MethodGet, wantAttempts: 2},
		{method: http.MethodPost, wantAttempts: 1, wantError: true},
	} {
		t.Run(test.method, func(t *testing.T) {
			client := newTestClient(t, "http://example.test")
			transport := &flakyTransport{}
			client.httpClient.Transport.(*authTransport).next = transport
			client.retryDelay = time.Millisecond

			_, err := client.Do(context.Background(), test.method, "/api/v1/status", nil)
			if test.wantError && err == nil {
				t.Fatal("expected request error")
			}
			if !test.wantError && err != nil {
				t.Fatalf("unexpected request error: %v", err)
			}
			if got := transport.attempts.Load(); got != test.wantAttempts {
				t.Fatalf("attempts = %d, want %d", got, test.wantAttempts)
			}
		})
	}
}

func TestClientHonorsContextTimeout(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		<-req.Context().Done()
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := client.Do(ctx, http.MethodGet, "/slow", nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context deadline exceeded", err)
	}
}

func TestClientEnforcesResponseBodyLimit(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("x", 33))
	}))
	defer server.Close()

	client, err := New(Config{
		BaseURL:          server.URL,
		APIKey:           syntheticAPIKey,
		UserAgent:        "terraform-provider-chaptarr/test",
		Timeout:          time.Second,
		MaxResponseBytes: 32,
	})
	if err != nil {
		t.Fatalf("New returned an error: %v", err)
	}
	_, err = client.Do(context.Background(), http.MethodGet, "/large", nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || !strings.Contains(apiErr.Message, "32-byte limit") {
		t.Fatalf("error = %v, want bounded-body APIError", err)
	}
}

func TestAPIErrorRecursivelyRedactsCredentialsAndQuery(t *testing.T) {
	t.Parallel()

	responseBody := `{
  "message": "Authorization: Bearer bearer-secret",
  "APIKey": "response-api-secret",
  "nested": {"providerSettingsCredentials": {"token": "nested-token-secret"}},
  "array": [{"PASSWORD": "password-secret"}],
  "echo": "test-api-key-sentinel-82df5"
}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Request-Id", "token=request-id-secret "+syntheticAPIKey)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, responseBody)
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	_, err := client.Do(context.Background(), http.MethodGet, "/api/v1/fail?trace=query-secret&safe=ok", nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %T %v, want *APIError", err, err)
	}
	if apiErr.Method != http.MethodGet || apiErr.Path != "/api/v1/fail" || apiErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("unexpected typed error fields: %#v", apiErr)
	}

	combined := apiErr.Error() + " " + apiErr.Message + " " + apiErr.RequestID
	for _, forbidden := range []string{
		syntheticAPIKey,
		"query-secret",
		"request-id-secret",
		"bearer-secret",
		"response-api-secret",
		"nested-token-secret",
		"password-secret",
	} {
		if strings.Contains(combined, forbidden) {
			t.Fatalf("error leaked %q: %s", forbidden, combined)
		}
	}
	if !strings.Contains(combined, "[REDACTED]") {
		t.Fatalf("error did not show redaction marker: %s", combined)
	}
}

func TestPlainTextErrorRedaction(t *testing.T) {
	t.Parallel()

	message := sanitizeResponseMessage([]byte(`password="prefix\"tail-secret" Digest username="person", response="digest-secret" {"api_key":"quoted-secret"`), syntheticAPIKey)
	for _, forbidden := range []string{"tail-secret", "person", "digest-secret", "quoted-secret"} {
		if strings.Contains(message, forbidden) {
			t.Fatalf("plain-text error leaked %q: %s", forbidden, message)
		}
	}
	if message != "request failed with a non-JSON response; content omitted" {
		t.Fatalf("malformed response message = %q, want generic omission", message)
	}
}

func TestJSONStringValuesWithCredentialMarkersAreFullyRedacted(t *testing.T) {
	t.Parallel()

	body := []byte(`{"message":"password=prefix\\\"tail-secret","detail":"Digest username=person response=digest-secret","safe":"ordinary validation failure"}`)
	message := sanitizeResponseMessage(body, syntheticAPIKey)
	for _, forbidden := range []string{"tail-secret", "person", "digest-secret"} {
		if strings.Contains(message, forbidden) {
			t.Fatalf("JSON error leaked %q: %s", forbidden, message)
		}
	}
	if !strings.Contains(message, `"message":"[REDACTED]"`) || !strings.Contains(message, `"detail":"[REDACTED]"`) {
		t.Fatalf("credential-bearing JSON strings were not fully redacted: %s", message)
	}
	if !strings.Contains(message, "ordinary validation failure") {
		t.Fatalf("safe JSON message was unexpectedly removed: %s", message)
	}
}

func TestSystemStatusRejectsMalformedJSONWithoutEchoingBody(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `not-json password=malformed-secret`)
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	_, err := client.SystemStatus(context.Background())
	if err == nil {
		t.Fatal("expected malformed JSON error")
	}
	if strings.Contains(err.Error(), "malformed-secret") {
		t.Fatalf("malformed response leaked in error: %v", err)
	}
}

func TestClientTLSAndTimeoutDefaultsAreBounded(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, "https://example.test")
	transport := client.httpClient.Transport.(*authTransport).next.(*http.Transport)
	if transport.TLSClientConfig.MinVersion != tls.VersionTLS12 {
		t.Fatalf("TLS minimum = %d, want TLS 1.2", transport.TLSClientConfig.MinVersion)
	}
	if transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("TLS verification unexpectedly disabled by default")
	}
	if client.timeout < MinRequestTimeout || client.timeout > MaxRequestTimeout {
		t.Fatalf("timeout %s is outside bounds", client.timeout)
	}
}

func TestNewHonorsExplicitInsecureSkipVerify(t *testing.T) {
	t.Parallel()

	client, err := New(Config{
		BaseURL:            "https://example.test",
		APIKey:             syntheticAPIKey,
		UserAgent:          "terraform-provider-chaptarr/test",
		InsecureSkipVerify: true,
		Timeout:            time.Second,
	})
	if err != nil {
		t.Fatalf("New returned an error: %v", err)
	}
	transport := client.httpClient.Transport.(*authTransport).next.(*http.Transport)
	if !transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("explicit insecure_skip_verify was not honored")
	}
}

func TestNewRejectsMissingCredentialsAndUnboundedSettings(t *testing.T) {
	t.Parallel()

	for name, config := range map[string]Config{
		"missing API key": {
			BaseURL:   "https://example.test",
			UserAgent: "terraform-provider-chaptarr/test",
		},
		"missing user agent": {
			BaseURL: "https://example.test",
			APIKey:  syntheticAPIKey,
		},
		"timeout below bound": {
			BaseURL:   "https://example.test",
			APIKey:    syntheticAPIKey,
			UserAgent: "terraform-provider-chaptarr/test",
			Timeout:   MinRequestTimeout - time.Nanosecond,
		},
		"timeout above bound": {
			BaseURL:   "https://example.test",
			APIKey:    syntheticAPIKey,
			UserAgent: "terraform-provider-chaptarr/test",
			Timeout:   MaxRequestTimeout + time.Nanosecond,
		},
		"response limit below bound": {
			BaseURL:          "https://example.test",
			APIKey:           syntheticAPIKey,
			UserAgent:        "terraform-provider-chaptarr/test",
			MaxResponseBytes: -1,
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := New(config); err == nil {
				t.Fatal("expected invalid client configuration to be rejected")
			}
		})
	}
}

func TestInvalidMethodErrorDoesNotEchoCallerInput(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, "https://example.test")
	_, err := client.Do(context.Background(), "INVALID-"+syntheticAPIKey, "/status", nil)
	if err == nil {
		t.Fatal("expected invalid method to be rejected")
	}
	if strings.Contains(err.Error(), syntheticAPIKey) {
		t.Fatalf("invalid method error leaked caller input: %v", err)
	}
}

func TestRetryDelayParsingIsBounded(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	if got := retryDelay("3600", now, time.Second, 5*time.Second); got != 5*time.Second {
		t.Fatalf("numeric Retry-After = %s, want 5s cap", got)
	}
	if got := retryDelay(now.Add(10*time.Second).Format(http.TimeFormat), now, time.Second, 5*time.Second); got != 5*time.Second {
		t.Fatalf("date Retry-After = %s, want 5s cap", got)
	}
}

func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	client, err := New(Config{
		BaseURL:   baseURL,
		APIKey:    syntheticAPIKey,
		UserAgent: "terraform-provider-chaptarr/test",
		Timeout:   time.Second,
	})
	if err != nil {
		t.Fatalf("New returned an error: %v", err)
	}
	return client
}

type flakyTransport struct {
	attempts atomic.Int32
}

func (t *flakyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.attempts.Add(1) == 1 {
		return nil, io.EOF
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{}`)),
		Request:    req,
	}, nil
}

func TestEffectivePortTreatsDefaultPortsAsEquivalent(t *testing.T) {
	t.Parallel()

	base, _ := url.Parse("https://example.test")
	candidate, _ := url.Parse("https://example.test:443/api")
	if !isAllowedURL(base, candidate) {
		t.Fatal("default HTTPS port should be same-origin")
	}
}
