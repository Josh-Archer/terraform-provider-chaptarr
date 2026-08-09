// Package client provides the hardened Chaptarr HTTP API client.
package client

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	// DefaultRequestTimeout bounds an entire API operation, including retries.
	DefaultRequestTimeout = 30 * time.Second
	// MinRequestTimeout is the shortest accepted API operation timeout.
	MinRequestTimeout = time.Second
	// MaxRequestTimeout is the longest accepted API operation timeout.
	MaxRequestTimeout = 5 * time.Minute
	// DefaultMaxResponseBodyBytes prevents unbounded response buffering.
	DefaultMaxResponseBodyBytes int64 = 2 << 20

	defaultMaxAttempts   = 3
	defaultRetryDelay    = 200 * time.Millisecond
	defaultMaxRetryAfter = 30 * time.Second
	maxErrorMessageBytes = 512
)

// Config controls creation of a Client.
type Config struct {
	BaseURL            string
	APIKey             string
	UserAgent          string
	InsecureSkipVerify bool
	Timeout            time.Duration
	MaxResponseBytes   int64
}

// Client is an immutable Chaptarr API client safe for concurrent use.
type Client struct {
	baseURL         *url.URL
	apiKey          string
	userAgent       string
	httpClient      *http.Client
	timeout         time.Duration
	maxResponseSize int64
	maxAttempts     int
	retryDelay      time.Duration
	maxRetryAfter   time.Duration
}

type authTransport struct {
	apiKey    string
	userAgent string
	next      http.RoundTripper
}

// Response is a successful Chaptarr response with a bounded body.
type Response struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

// APIError is a sanitized request or API failure. It never contains a raw
// request body, response body, query string, or configured credential.
type APIError struct {
	Method     string
	Path       string
	StatusCode int
	RequestID  string
	Message    string
	cause      error
}

func (e *APIError) Error() string {
	if e == nil {
		return "<nil>"
	}

	status := ""
	if e.StatusCode != 0 {
		status = fmt.Sprintf(" status=%d", e.StatusCode)
	}
	requestID := ""
	if e.RequestID != "" {
		requestID = " request_id=" + e.RequestID
	}
	message := ""
	if e.Message != "" {
		message = ": " + e.Message
	}
	return fmt.Sprintf("chaptarr API %s %s%s%s%s", e.Method, e.Path, status, requestID, message)
}

func (e *APIError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// SystemStatus is the non-sensitive version information exposed by Chaptarr.
type SystemStatus struct {
	ApplicationName string `json:"appName"`
	Version         string `json:"version"`
}

// New validates cfg and constructs a client without performing network I/O.
func New(cfg Config) (*Client, error) {
	baseURL, err := ParseBaseURL(cfg.BaseURL)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("API key must not be empty")
	}
	if strings.TrimSpace(cfg.UserAgent) == "" {
		return nil, errors.New("user agent must not be empty")
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = DefaultRequestTimeout
	}
	if timeout < MinRequestTimeout || timeout > MaxRequestTimeout {
		return nil, fmt.Errorf("request timeout must be between %s and %s", MinRequestTimeout, MaxRequestTimeout)
	}

	maxResponseSize := cfg.MaxResponseBytes
	if maxResponseSize == 0 {
		maxResponseSize = DefaultMaxResponseBodyBytes
	}
	if maxResponseSize < 1 {
		return nil, errors.New("maximum response body size must be positive")
	}

	baseTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, errors.New("default HTTP transport has an unexpected type")
	}
	transport := baseTransport.Clone()
	transport.TLSClientConfig = &tls.Config{
		MinVersion: tls.VersionTLS12,
		// #nosec G402 -- certificate verification is disabled only by an explicit provider setting.
		InsecureSkipVerify: cfg.InsecureSkipVerify,
	}

	auth := &authTransport{
		apiKey:    cfg.APIKey,
		userAgent: strings.TrimSpace(cfg.UserAgent),
		next:      transport,
	}

	c := &Client{
		baseURL:         baseURL,
		apiKey:          cfg.APIKey,
		userAgent:       strings.TrimSpace(cfg.UserAgent),
		timeout:         timeout,
		maxResponseSize: maxResponseSize,
		maxAttempts:     defaultMaxAttempts,
		retryDelay:      defaultRetryDelay,
		maxRetryAfter:   defaultMaxRetryAfter,
	}
	c.httpClient = &http.Client{
		Transport: auth,
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) != 0 && !shouldRetryMethod(via[0].Method) {
				return errors.New("redirects are not allowed for mutating Chaptarr requests")
			}
			if !isAllowedURL(baseURL, req.URL) || hasSensitiveQuery(req.URL, cfg.APIKey) {
				return errors.New("redirect target is outside the configured Chaptarr base URL")
			}
			return nil
		},
	}

	return c, nil
}

// ParseBaseURL validates and normalizes a Chaptarr base URL. Reverse-proxy
// subpaths are preserved and a trailing slash is removed.
func ParseBaseURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("base URL must not be empty")
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, errors.New("base URL is not a valid URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("base URL scheme must be http or https")
	}
	if parsed.Host == "" || parsed.Hostname() == "" {
		return nil, errors.New("base URL must include a host")
	}
	if parsed.User != nil {
		return nil, errors.New("base URL must not include user information")
	}
	if parsed.RawQuery != "" {
		return nil, errors.New("base URL must not include a query string")
	}
	if parsed.Fragment != "" {
		return nil, errors.New("base URL must not include a fragment")
	}
	if parsed.Opaque != "" {
		return nil, errors.New("base URL must not be opaque")
	}

	cleanedPath, escapedPath, err := normalizeBasePath(parsed)
	if err != nil {
		return nil, err
	}
	parsed.Path = cleanedPath
	parsed.RawPath = escapedPath
	return parsed, nil
}

func normalizeBasePath(parsed *url.URL) (string, string, error) {
	escaped := parsed.EscapedPath()
	decoded, err := url.PathUnescape(escaped)
	if err != nil {
		return "", "", errors.New("base URL path has invalid escaping")
	}
	if hasDotSegment(escaped) {
		return "", "", errors.New("base URL path must not contain dot segments")
	}

	decoded = strings.TrimSuffix(decoded, "/")
	if decoded == "/" {
		decoded = ""
	}
	escaped = strings.TrimSuffix(escaped, "/")
	if escaped == "/" {
		escaped = ""
	}
	if escaped == decoded {
		escaped = ""
	}
	return decoded, escaped, nil
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	cloned := req.Clone(req.Context())
	cloned.Header = req.Header.Clone()
	cloned.Header.Set("X-Api-Key", t.apiKey)
	cloned.Header.Set("Accept", "application/json")
	cloned.Header.Set("User-Agent", t.userAgent)
	if cloned.Body != nil {
		cloned.Header.Set("Content-Type", "application/json")
	}
	return t.next.RoundTrip(cloned)
}

// Do sends an API request and returns only successful 2xx responses.
func (c *Client) Do(ctx context.Context, method, requestPath string, body []byte) (*Response, error) {
	method = strings.ToUpper(strings.TrimSpace(method))
	if !isSupportedMethod(method) {
		return nil, &APIError{Method: "[invalid-method]", Path: "[unresolved]", Message: "HTTP method is not supported"}
	}

	requestURL, safePath, err := c.resolve(requestPath)
	if err != nil {
		return nil, &APIError{Method: method, Path: safePath, Message: err.Error()}
	}

	requestCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	var lastErr error
	for attempt := 1; attempt <= c.maxAttempts; attempt++ {
		var requestBody io.Reader
		if len(body) != 0 {
			requestBody = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(requestCtx, method, requestURL.String(), requestBody)
		if err != nil {
			return nil, &APIError{Method: method, Path: safePath, Message: "could not construct request"}
		}

		res, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			if shouldRetryMethod(method) && attempt < c.maxAttempts && isTransientNetworkError(requestCtx, err) {
				if waitErr := waitForRetry(requestCtx, c.retryDelay*time.Duration(attempt)); waitErr != nil {
					return nil, c.networkError(method, safePath, waitErr)
				}
				continue
			}
			return nil, c.networkError(method, safePath, err)
		}

		if shouldRetryMethod(method) && attempt < c.maxAttempts && isRetryableStatus(res.StatusCode) {
			discardAndClose(res.Body)
			delay := retryDelay(res.Header.Get("Retry-After"), time.Now(), c.retryDelay*time.Duration(attempt), c.maxRetryAfter)
			if waitErr := waitForRetry(requestCtx, delay); waitErr != nil {
				return nil, c.networkError(method, safePath, waitErr)
			}
			continue
		}

		return c.response(method, safePath, res)
	}

	return nil, c.networkError(method, safePath, lastErr)
}

// SystemStatus retrieves Chaptarr application and version information.
func (c *Client) SystemStatus(ctx context.Context) (SystemStatus, error) {
	res, err := c.Do(ctx, http.MethodGet, "/api/v1/system/status", nil)
	if err != nil {
		return SystemStatus{}, err
	}

	var status SystemStatus
	if err := json.Unmarshal(res.Body, &status); err != nil {
		return SystemStatus{}, &APIError{
			Method:     http.MethodGet,
			Path:       "/api/v1/system/status",
			StatusCode: res.StatusCode,
			RequestID:  requestID(res.Header, c.apiKey),
			Message:    "response was not valid JSON",
		}
	}
	return status, nil
}

func (c *Client) response(method, safePath string, res *http.Response) (*Response, error) {
	defer res.Body.Close()
	body, tooLarge, err := readBounded(res.Body, c.maxResponseSize)
	if err != nil {
		return nil, &APIError{
			Method:     method,
			Path:       safePath,
			StatusCode: res.StatusCode,
			RequestID:  requestID(res.Header, c.apiKey),
			Message:    "could not read response body",
		}
	}
	if tooLarge {
		return nil, &APIError{
			Method:     method,
			Path:       safePath,
			StatusCode: res.StatusCode,
			RequestID:  requestID(res.Header, c.apiKey),
			Message:    fmt.Sprintf("response body exceeded the %d-byte limit", c.maxResponseSize),
		}
	}

	response := &Response{StatusCode: res.StatusCode, Header: res.Header.Clone(), Body: body}
	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		return nil, &APIError{
			Method:     method,
			Path:       safePath,
			StatusCode: res.StatusCode,
			RequestID:  requestID(res.Header, c.apiKey),
			Message:    sanitizeResponseMessage(body, c.apiKey),
		}
	}
	return response, nil
}

func (c *Client) networkError(method, safePath string, err error) error {
	message := "request failed"
	var cause error
	if errors.Is(err, context.DeadlineExceeded) {
		message = "request timed out"
		cause = context.DeadlineExceeded
	} else if errors.Is(err, context.Canceled) {
		message = "request was canceled"
		cause = context.Canceled
	}
	return &APIError{Method: method, Path: safePath, Message: message, cause: cause}
}

func (c *Client) resolve(rawPath string) (*url.URL, string, error) {
	rawPath = strings.TrimSpace(rawPath)
	if rawPath == "" {
		return nil, "[invalid-path]", errors.New("request path must not be empty")
	}

	parsed, err := url.Parse(rawPath)
	if err != nil {
		return nil, "[invalid-path]", errors.New("request path is invalid")
	}
	if parsed.Fragment != "" || parsed.User != nil {
		return nil, safePath(parsed, c.apiKey), errors.New("request path must not include user information or a fragment")
	}
	if hasSensitiveQuery(parsed, c.apiKey) {
		return nil, safePath(parsed, c.apiKey), errors.New("request query must not contain credentials")
	}

	if parsed.IsAbs() {
		if !isAllowedURL(c.baseURL, parsed) {
			return nil, safePath(parsed, c.apiKey), errors.New("absolute request URL is outside the configured Chaptarr base URL")
		}
		return parsed, safePath(parsed, c.apiKey), nil
	}
	if parsed.Host != "" || strings.HasPrefix(rawPath, "//") {
		return nil, safePath(parsed, c.apiKey), errors.New("network-path references are not allowed")
	}
	if parsed.Path == "" || hasDotSegment(parsed.EscapedPath()) {
		return nil, safePath(parsed, c.apiKey), errors.New("request path must not contain dot segments")
	}

	requestDecodedPath := parsed.Path
	if !strings.HasPrefix(requestDecodedPath, "/") {
		requestDecodedPath = "/" + requestDecodedPath
	}
	requestEscapedPath := parsed.EscapedPath()
	if !strings.HasPrefix(requestEscapedPath, "/") {
		requestEscapedPath = "/" + requestEscapedPath
	}

	joined := *c.baseURL
	joined.Path = strings.TrimSuffix(c.baseURL.Path, "/") + requestDecodedPath
	joined.RawPath = strings.TrimSuffix(c.baseURL.EscapedPath(), "/") + requestEscapedPath
	if joined.RawPath == joined.Path {
		joined.RawPath = ""
	}
	joined.RawQuery = parsed.RawQuery
	joined.Fragment = ""
	return &joined, safePath(&joined, c.apiKey), nil
}

func isAllowedURL(baseURL, candidate *url.URL) bool {
	if baseURL == nil || candidate == nil || candidate.User != nil {
		return false
	}
	if hasDotSegment(candidate.EscapedPath()) {
		return false
	}
	if !strings.EqualFold(baseURL.Scheme, candidate.Scheme) || !strings.EqualFold(baseURL.Hostname(), candidate.Hostname()) {
		return false
	}
	if effectivePort(baseURL) != effectivePort(candidate) {
		return false
	}

	basePath := strings.TrimSuffix(baseURL.Path, "/")
	if basePath == "" {
		return true
	}
	candidatePath := path.Clean(candidate.Path)
	return candidatePath == basePath || strings.HasPrefix(candidatePath, basePath+"/")
}

func hasSensitiveQuery(candidate *url.URL, secret string) bool {
	if candidate == nil {
		return false
	}
	query, err := url.ParseQuery(candidate.RawQuery)
	if err != nil {
		// Malformed queries cannot be inspected reliably. Reject them rather
		// than allowing url.Values to silently discard credential-bearing data.
		return true
	}
	for key, values := range query {
		if isSensitiveKey(key) {
			return true
		}
		if secret == "" {
			continue
		}
		for _, value := range values {
			if strings.Contains(value, secret) {
				return true
			}
		}
	}
	return false
}

func effectivePort(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}
	if strings.EqualFold(u.Scheme, "https") {
		return "443"
	}
	return "80"
}

func safePath(u *url.URL, secret string) string {
	if u == nil || u.EscapedPath() == "" {
		return "/"
	}
	return truncateMessage(redactKnownSecret(u.EscapedPath(), secret), maxErrorMessageBytes)
}

func hasDotSegment(value string) bool {
	decoded, err := url.PathUnescape(value)
	if err != nil {
		return true
	}
	for _, segment := range strings.Split(strings.ReplaceAll(decoded, `\`, "/"), "/") {
		if segment == "." || segment == ".." {
			return true
		}
	}
	return false
}

func shouldRetryMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead
}

func isSupportedMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func isRetryableStatus(status int) bool {
	switch status {
	case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func isTransientNetworkError(ctx context.Context, err error) bool {
	if err == nil || ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.ECONNABORTED)
}

func retryDelay(value string, now time.Time, fallback, maximum time.Duration) time.Duration {
	value = strings.TrimSpace(value)
	delay := fallback
	if seconds, err := strconv.ParseFloat(value, 64); err == nil && seconds >= 0 {
		delay = time.Duration(seconds * float64(time.Second))
	} else if retryAt, err := http.ParseTime(value); err == nil {
		delay = retryAt.Sub(now)
		if delay < 0 {
			delay = 0
		}
	}
	if delay > maximum {
		return maximum
	}
	return delay
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func discardAndClose(body io.ReadCloser) {
	_, _ = io.CopyN(io.Discard, body, 8*1024)
	_ = body.Close()
}

func readBounded(reader io.Reader, limit int64) ([]byte, bool, error) {
	body, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(body)) > limit {
		return nil, true, nil
	}
	return body, false, nil
}

func requestID(header http.Header, secret string) string {
	for _, name := range []string{"X-Request-Id", "Request-Id", "X-Correlation-Id"} {
		if value := strings.TrimSpace(header.Get(name)); value != "" {
			return truncateMessage(sanitizePlainText(value, secret), 128)
		}
	}
	return ""
}
