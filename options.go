package typesafe

import (
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultBaseURL is the public TypeSafe API endpoint.
const DefaultBaseURL = "https://api.typesafe.ai"

const userAgent = "typesafe-go/0.1"

// Option configures a Client during construction.
type Option func(*config) error

type config struct {
	baseURL        *url.URL
	httpClient     http.Client
	defaultModel   string
	attemptTimeout time.Duration
	retry          RetryPolicy
	responseLimit  int64
}

func defaultConfig() config {
	u, _ := url.Parse(DefaultBaseURL)
	return config{
		baseURL: u, httpClient: http.Client{}, defaultModel: "jev-latest",
		attemptTimeout: 10 * time.Second, retry: DefaultRetryPolicy(), responseLimit: 4 << 20,
	}
}

// WithBaseURL changes the API endpoint. HTTP is accepted only for loopback hosts.
func WithBaseURL(raw string) Option {
	return func(c *config) error {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" || u.Path != "" && u.Path != "/" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
			return errors.New("typesafe: invalid base URL")
		}
		if u.Scheme != "https" && !(u.Scheme == "http" && isLoopbackHost(u.Hostname())) {
			return errors.New("typesafe: base URL must use HTTPS (HTTP is allowed only on loopback)")
		}
		u.Path = strings.TrimSuffix(u.Path, "/")
		c.baseURL = u
		return nil
	}
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// WithHTTPClient copies client and disables redirects on the copy. The caller's
// transport must be safe for concurrent use when the resulting Client is shared.
func WithHTTPClient(client *http.Client) Option {
	return func(c *config) error {
		if client == nil {
			return errors.New("typesafe: HTTP client is nil")
		}
		c.httpClient = *client
		return nil
	}
}

// WithDefaultModel sets the model used when SystemOneRequest.Model is empty.
func WithDefaultModel(model string) Option {
	return func(c *config) error {
		if strings.TrimSpace(model) == "" {
			return errors.New("typesafe: default model is blank")
		}
		c.defaultModel = model
		return nil
	}
}

// WithAttemptTimeout bounds each HTTP attempt, including reading its body.
func WithAttemptTimeout(timeout time.Duration) Option {
	return func(c *config) error {
		if timeout <= 0 {
			return errors.New("typesafe: attempt timeout must be positive")
		}
		c.attemptTimeout = timeout
		return nil
	}
}

// WithRetryPolicy sets the complete retry policy. Fields are not merged with
// defaults; to disable retries while retaining defaults, set MaxRetries to zero
// on the value returned by DefaultRetryPolicy.
func WithRetryPolicy(policy RetryPolicy) Option {
	return func(c *config) error {
		if err := policy.validate(); err != nil {
			return err
		}
		c.retry = policy
		return nil
	}
}

// WithResponseLimit sets the maximum decompressed success or error body size.
func WithResponseLimit(limit int64) Option {
	return func(c *config) error {
		if limit <= 0 || limit == 1<<63-1 {
			return errors.New("typesafe: response limit must be positive and below MaxInt64")
		}
		c.responseLimit = limit
		return nil
	}
}

// Client is safe for concurrent calls when its HTTP transport is safe.
type Client struct {
	apiKey         string
	baseURL        *url.URL
	httpClient     http.Client
	defaultModel   string
	attemptTimeout time.Duration
	retry          RetryPolicy
	responseLimit  int64
}

// NewClient constructs a client without performing network access. The API key
// is explicit; the SDK never reads it from the environment.
func NewClient(apiKey string, opts ...Option) (*Client, error) {
	if strings.TrimSpace(apiKey) == "" || strings.ContainsAny(apiKey, "\r\n") {
		return nil, &ValidationError{Field: "api_key", Reason: "invalid value"}
	}
	cfg := defaultConfig()
	for _, opt := range opts {
		if opt == nil {
			return nil, errors.New("typesafe: nil option")
		}
		if err := opt(&cfg); err != nil {
			return nil, err
		}
	}
	if err := cfg.retry.validate(); err != nil {
		return nil, err
	}
	cfg.httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	u := *cfg.baseURL
	return &Client{apiKey: apiKey, baseURL: &u, httpClient: cfg.httpClient, defaultModel: cfg.defaultModel,
		attemptTimeout: cfg.attemptTimeout, retry: cfg.retry, responseLimit: cfg.responseLimit}, nil
}

// String returns a redacted client description.
func (c *Client) String() string { return "typesafe.Client{redacted}" }

// GoString returns a redacted Go-syntax client description.
func (c *Client) GoString() string { return "typesafe.Client{redacted}" }

// LogValue returns a redacted structured logging value.
func (c *Client) LogValue() slog.Value {
	return slog.StringValue("typesafe.Client{redacted}")
}

// MarshalJSON returns a redacted client representation.
func (c *Client) MarshalJSON() ([]byte, error) { return []byte(`"typesafe.Client{redacted}"`), nil }

func nonRetryableTransport(err error) bool {
	var certErr x509.UnknownAuthorityError
	var hostErr x509.HostnameError
	var certInvalid x509.CertificateInvalidError
	return errors.As(err, &certErr) || errors.As(err, &hostErr) || errors.As(err, &certInvalid)
}

var _ fmt.Stringer = (*Client)(nil)
