package downloader

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/douglasmiranda/godownload/internal/config"
	"github.com/douglasmiranda/godownload/internal/security"
)

const (
	maxRedirects  = 10
	maxRetryDelay = 5 * time.Minute
	// maxErrorBodyDrain bounds how much of a non-2xx response is read so the
	// connection can be reused without downloading large error pages.
	maxErrorBodyDrain = 64 << 10
)

// defaultRetryHTTPCodes are retried when retry.on_http_code is not set.
var defaultRetryHTTPCodes = []int{
	http.StatusRequestTimeout,
	http.StatusTooEarly,
	http.StatusTooManyRequests,
	http.StatusInternalServerError,
	http.StatusBadGateway,
	http.StatusServiceUnavailable,
	http.StatusGatewayTimeout,
}

// taskClient performs the HTTP requests of a single task. Its configuration
// (timeouts, TLS, redirects, credentials) is task-level, and sharing one
// transport lets connections be reused across the task's downloads.
type taskClient struct {
	http        *http.Client
	task        *config.DownloadTask
	idleTimeout time.Duration
	// trusted lists the hosts (and their subdomains) that may receive the
	// task's credentials.
	trusted []string
	debugf  func(format string, args ...any)
}

func newTaskClient(task *config.DownloadTask, concurrency int, debugf func(string, ...any)) *taskClient {
	idle := time.Duration(config.DefaultTimeoutSeconds) * time.Second
	if task.Timeout > 0 {
		idle = time.Duration(task.Timeout) * time.Second
	}
	if debugf == nil {
		debugf = func(string, ...any) {}
	}

	transport := newTransport(concurrency, false)

	c := &taskClient{
		task:        task,
		idleTimeout: idle,
		trusted:     append([]string{}, task.Security.AllowedDomains...),
		debugf:      debugf,
	}
	var rt http.RoundTripper = transport
	if task.Security.AllowSelfSigned {
		rt = &trustedHostTransport{trusted: newTransport(concurrency, true), other: transport, trusts: c.trusts}
	}
	c.http = &http.Client{
		Transport:     rt,
		CheckRedirect: c.checkRedirect,
	}
	return c
}

func newTransport(concurrency int, skipVerify bool) *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = &tls.Config{}
	} else {
		transport.TLSClientConfig = transport.TLSClientConfig.Clone()
	}
	transport.TLSClientConfig.InsecureSkipVerify = skipVerify
	// Save response bodies byte-for-byte. With compression enabled Go would
	// transparently gunzip responses, corrupting files such as .gz archives
	// served with Content-Encoding: gzip.
	transport.DisableCompression = true
	transport.MaxIdleConnsPerHost = max(concurrency, 2)
	return transport
}

// trustedHostTransport limits allow_self_signed_cert to the trusted hosts, so
// hosts reached through discovered links or redirects keep certificate
// verification.
type trustedHostTransport struct {
	trusted http.RoundTripper
	other   http.RoundTripper
	trusts  func(*url.URL) bool
}

func (t *trustedHostTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.trusts(req.URL) {
		return t.trusted.RoundTrip(req)
	}
	return t.other.RoundTrip(req)
}

// trustURLs allows credentials to be sent to the hosts of the given URLs.
// It must be called before requests are made concurrently.
func (c *taskClient) trustURLs(rawURLs ...string) {
	for _, raw := range rawURLs {
		u, err := url.Parse(raw)
		if err != nil || u.Hostname() == "" {
			continue
		}
		if !slices.Contains(c.trusted, u.Hostname()) {
			c.trusted = append(c.trusted, u.Hostname())
		}
	}
}

func (c *taskClient) trusts(u *url.URL) bool {
	return security.HostMatchesAny(u.Hostname(), c.trusted)
}

func (c *taskClient) checkRedirect(req *http.Request, via []*http.Request) error {
	if !followRedirectsEnabled(c.task) {
		return http.ErrUseLastResponse
	}
	if len(via) >= maxRedirects {
		return permanent(fmt.Errorf("stopped after %d redirects", maxRedirects))
	}
	if !security.IsURLAllowed(req.URL.String(), c.task.Security.AllowInsecureHTTP, c.task.Security.AllowedDomains) {
		return permanent(fmt.Errorf("redirect target blocked by security policy: %s", req.URL.String()))
	}
	if c.task.Auth != nil && !c.trusts(req.URL) {
		stripCredentials(req, c.task.Auth)
	}
	c.debugf("redirect %s -> %s", via[len(via)-1].URL, req.URL)
	return nil
}

// responseHandler consumes a successful response. body must be used instead
// of resp.Body: it enforces the idle timeout and records read errors so they
// can be retried.
type responseHandler func(resp *http.Response, body io.Reader) error

// get performs a GET request, retrying according to the task's retry
// configuration. handle is only called for 2xx responses.
func (c *taskClient) get(ctx context.Context, rawURL string, handle responseHandler) error {
	retry := c.task.Retry
	attempts := 1
	if retry.Enabled {
		attempts = max(retry.Config.Retries+1, 1)
	}

	var lastErr error
	for attempt := 1; ; attempt++ {
		retryAfter, err := c.getOnce(ctx, rawURL, handle)
		if err == nil {
			return nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !retry.Enabled || attempt >= attempts || !shouldRetry(retry.Config, err) {
			break
		}
		delay := max(backoffDelay(retry.Config, attempt), retryAfter)
		c.debugf("retry %d/%d for %s in %s: %v", attempt, attempts-1, rawURL, delay, err)
		if err := sleepCtx(ctx, delay); err != nil {
			return err
		}
	}

	if code := codeFromError(lastErr); shouldStopOnHTTPCode(c.task, code) {
		return fmt.Errorf("stop_all_tasks.on_http_code matched %d for %s: %w", code, rawURL, lastErr)
	}
	return lastErr
}

func (c *taskClient) getOnce(ctx context.Context, rawURL string, handle responseHandler) (time.Duration, error) {
	reqCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	idle := startIdleTimer(c.idleTimeout, cancel)
	defer idle.stop()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return 0, permanent(fmt.Errorf("build request: %w", err))
	}
	withheld := false
	if c.task.Auth != nil {
		if c.trusts(req.URL) {
			if err := applyAuth(req, c.task.Auth); err != nil {
				return 0, permanent(err)
			}
		} else {
			withheld = true
		}
	}

	c.debugf("GET %s", rawURL)
	resp, err := c.http.Do(req)
	if err != nil {
		if idle.fired.Load() {
			return 0, idle.timeoutError(rawURL)
		}
		return 0, classifyNetworkError(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrorBodyDrain))
		return parseRetryAfter(resp.Header.Get("Retry-After"), time.Now()), httpStatusError{
			Code: resp.StatusCode,
			Err:  httpStatusFailure(c.task, rawURL, resp, withheld),
		}
	}

	body := &guardedBody{r: resp.Body, idle: idle, url: rawURL}
	if err := handle(resp, body); err != nil {
		if body.err != nil {
			return 0, body.err
		}
		return 0, permanent(err)
	}
	return 0, nil
}

// guardedBody resets the idle timer on progress and keeps the first read
// error, so callers can tell network failures from their own errors.
type guardedBody struct {
	r    io.Reader
	idle *idleTimer
	url  string
	err  error
}

func (b *guardedBody) Read(p []byte) (int, error) {
	n, err := b.r.Read(p)
	if n > 0 {
		b.idle.reset()
	}
	if err != nil && err != io.EOF {
		if b.idle.fired.Load() {
			err = b.idle.timeoutError(b.url)
		} else {
			err = fmt.Errorf("ConnectionError: read response body from %s: %w", b.url, err)
		}
		if b.err == nil {
			b.err = err
		}
	}
	return n, err
}

// limitReader fails with err once more than n bytes have been read.
type limitReader struct {
	r   io.Reader
	n   int64
	err error
}

func newLimitReader(r io.Reader, n int64, err error) io.Reader {
	return &limitReader{r: r, n: n, err: err}
}

func (l *limitReader) Read(p []byte) (int, error) {
	if l.n < 0 {
		return 0, l.err
	}
	if int64(len(p)) > l.n+1 {
		p = p[:l.n+1]
	}
	n, err := l.r.Read(p)
	l.n -= int64(n)
	if l.n < 0 {
		return n, l.err
	}
	return n, err
}

// idleTimer cancels a request when no data arrives for d. It covers
// connecting, waiting for the response and every pause while reading the
// body, so large downloads are not limited by a total deadline.
type idleTimer struct {
	d     time.Duration
	timer *time.Timer
	fired atomic.Bool
}

func startIdleTimer(d time.Duration, cancel context.CancelFunc) *idleTimer {
	t := &idleTimer{d: d}
	t.timer = time.AfterFunc(d, func() {
		t.fired.Store(true)
		cancel()
	})
	return t
}

func (t *idleTimer) reset() { t.timer.Reset(t.d) }
func (t *idleTimer) stop()  { t.timer.Stop() }

func (t *idleTimer) timeoutError(rawURL string) error {
	return fmt.Errorf("TimeoutError: no data received for %s from %s", t.d, rawURL)
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func applyAuth(req *http.Request, auth *config.AuthConfig) error {
	if auth == nil {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(auth.Type)) {
	case "basic":
		req.SetBasicAuth(auth.Username, auth.Password)
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+auth.Token)
	case "api_key":
		switch strings.ToLower(strings.TrimSpace(auth.KeyIn)) {
		case "", "header":
			req.Header.Set(auth.KeyName, auth.KeyValue)
		case "query":
			q := req.URL.Query()
			q.Set(auth.KeyName, auth.KeyValue)
			req.URL.RawQuery = q.Encode()
		case "cookie":
			req.AddCookie(&http.Cookie{Name: auth.KeyName, Value: auth.KeyValue})
		default:
			return fmt.Errorf("unsupported auth.key_in: %s", auth.KeyIn)
		}
	default:
		return fmt.Errorf("unsupported auth.type: %s", auth.Type)
	}
	return nil
}

// stripCredentials removes credentials copied from the original request when
// a redirect leaves the trusted hosts. Go already drops Authorization and
// Cookie for some cross-domain redirects, but not custom API key headers.
func stripCredentials(req *http.Request, auth *config.AuthConfig) {
	req.Header.Del("Authorization")
	req.Header.Del("Cookie")
	if strings.EqualFold(strings.TrimSpace(auth.Type), "api_key") && strings.TrimSpace(auth.KeyName) != "" {
		switch strings.ToLower(strings.TrimSpace(auth.KeyIn)) {
		case "", "header":
			req.Header.Del(auth.KeyName)
		case "query":
			q := req.URL.Query()
			if q.Has(auth.KeyName) {
				q.Del(auth.KeyName)
				req.URL.RawQuery = q.Encode()
			}
		}
	}
}

func shouldRetry(cfg config.RetryConfig, err error) bool {
	if err == nil {
		return false
	}
	var perm permanentError
	if errors.As(err, &perm) || isCertificateError(err) {
		return false
	}
	if code := codeFromError(err); code != 0 {
		codes := cfg.OnHTTPCode
		if len(codes) == 0 {
			codes = defaultRetryHTTPCodes
		}
		return slices.Contains(codes, code)
	}
	return cfg.OnRuntimeError
}

func isCertificateError(err error) bool {
	var unknownAuthority x509.UnknownAuthorityError
	var invalid x509.CertificateInvalidError
	var hostname x509.HostnameError
	var verification *tls.CertificateVerificationError
	return errors.As(err, &unknownAuthority) || errors.As(err, &invalid) ||
		errors.As(err, &hostname) || errors.As(err, &verification)
}

func backoffDelay(cfg config.RetryConfig, attempt int) time.Duration {
	if cfg.Delay <= 0 {
		return 0
	}
	d := time.Duration(cfg.Delay) * time.Second
	if cfg.ExponentialBackoff != nil && *cfg.ExponentialBackoff {
		for i := 1; i < attempt && d < maxRetryDelay; i++ {
			d *= 2
		}
	}
	return min(d, maxRetryDelay)
}

// parseRetryAfter returns the delay requested by a Retry-After header, given
// either in seconds or as an HTTP date.
func parseRetryAfter(v string, now time.Time) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	var d time.Duration
	if secs, err := strconv.Atoi(v); err == nil {
		d = time.Duration(secs) * time.Second
	} else if t, err := http.ParseTime(v); err == nil {
		d = t.Sub(now)
	}
	return min(max(d, 0), maxRetryDelay)
}

func classifyNetworkError(err error) error {
	var perm permanentError
	if errors.As(err, &perm) {
		return perm
	}
	var ue *url.Error
	if errors.As(err, &ue) && ue.Timeout() {
		return fmt.Errorf("TimeoutError: %w", err)
	}
	return fmt.Errorf("ConnectionError: %w", err)
}

func followRedirectsEnabled(task *config.DownloadTask) bool {
	if task != nil && task.Security.FollowRedirects != nil {
		return *task.Security.FollowRedirects
	}
	return true
}

func httpStatusFailure(task *config.DownloadTask, rawURL string, resp *http.Response, credentialsWithheld bool) error {
	if resp.StatusCode >= 300 && resp.StatusCode <= 399 && !followRedirectsEnabled(task) {
		msg := fmt.Sprintf("URL returned HTTP redirect %d but security.follow_redirects is false", resp.StatusCode)
		if loc := strings.TrimSpace(resp.Header.Get("Location")); loc != "" {
			msg += fmt.Sprintf(" (Location: %s)", loc)
		}
		return fmt.Errorf("HTTPError: %s", msg)
	}
	msg := fmt.Sprintf("HTTPError: request failed with status %d for %s", resp.StatusCode, rawURL)
	if credentialsWithheld && (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) {
		host := rawURL
		if u, err := url.Parse(rawURL); err == nil {
			host = u.Hostname()
		}
		msg += fmt.Sprintf(" (auth was not sent to %s; add it to security.allowed_domains to send credentials there)", host)
	}
	return errors.New(msg)
}

type httpStatusError struct {
	Code int
	Err  error
}

func (e httpStatusError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return fmt.Sprintf("HTTPError: request failed with status %d", e.Code)
}

func (e httpStatusError) Unwrap() error {
	return e.Err
}

func codeFromError(err error) int {
	var hsErr httpStatusError
	if err == nil || !errors.As(err, &hsErr) {
		return 0
	}
	return hsErr.Code
}

// permanentError marks failures that cannot succeed on retry, such as
// security policy violations or local storage errors.
type permanentError struct {
	err error
}

func (e permanentError) Error() string { return e.err.Error() }
func (e permanentError) Unwrap() error { return e.err }

func permanent(err error) error {
	var perm permanentError
	if err == nil || errors.As(err, &perm) {
		return err
	}
	return permanentError{err: err}
}
