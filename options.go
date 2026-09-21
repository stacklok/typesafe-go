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
	apiKey         string
	apiKeySet      bool
	httpClientSet  bool
	authClientSet  bool
}

func defaultConfig() config {
	u, _ := url.Parse(DefaultBaseURL)
	return config{
		baseURL: u, httpClient: http.Client{}, defaultModel: "jev-latest",
		attemptTimeout: 10 * time.Second, retry: DefaultRetryPolicy(), responseLimit: 4 << 20,
	}
}

// WithAPIKey configures SDK-managed bearer authentication. The key is never
// read from the environment. It may be combined with one WithHTTPClient.
// It cannot be combined with WithAuthenticatedHTTPClient.
func WithAPIKey(apiKey string) Option {
	return func(c *config) error {
		if c.apiKeySet {
			return errors.New("typesafe: WithAPIKey configured more than once; use one WithAPIKey")
		}
		if c.authClientSet {
			return errors.New("typesafe: WithAPIKey conflicts with WithAuthenticatedHTTPClient; choose one authentication strategy")
		}
		if strings.TrimSpace(apiKey) == "" || strings.ContainsAny(apiKey, "\r\n") {
			return &ValidationError{Field: "api_key", Reason: "invalid value"}
		}
		c.apiKey = apiKey
		c.apiKeySet = true
		return nil
	}
}

// WithBaseURL changes the API endpoint while preserving any configured path
// prefix. HTTP is accepted only for loopback hosts.
func WithBaseURL(raw string) Option {
	return func(c *config) error {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" || u.Scheme == "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.User != nil || strings.Contains(raw, "#") {
			return errors.New("typesafe: invalid base URL")
		}
		if u.Scheme != "https" && !(u.Scheme == "http" && isLoopbackHost(u.Hostname())) {
			return errors.New("typesafe: base URL must use HTTPS (HTTP is allowed only on loopback)")
		}
		for _, segment := range strings.Split(u.Path, "/") {
			if segment == "." || segment == ".." {
				return errors.New("typesafe: invalid base URL")
			}
		}
		if strings.HasSuffix(u.EscapedPath(), "/") {
			u.Path = strings.TrimSuffix(u.Path, "/")
			if u.RawPath != "" {
				u.RawPath = strings.TrimSuffix(u.RawPath, "/")
			}
		}
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

// WithHTTPClient copies a client for transport customization in API-key mode.
// The transport remains shared; the caller owns idle-connection cleanup. The SDK
// does not add a Close method. It does not authenticate: use it with WithAPIKey.
// It cannot be combined with WithAuthenticatedHTTPClient.
func WithHTTPClient(client *http.Client) Option {
	return func(c *config) error {
		if client == nil {
			return errors.New("typesafe: WithHTTPClient client is nil")
		}
		if c.httpClientSet {
			return errors.New("typesafe: WithHTTPClient configured more than once; use one WithHTTPClient")
		}
		if c.authClientSet {
			return errors.New("typesafe: WithHTTPClient conflicts with WithAuthenticatedHTTPClient; choose one client option")
		}
		c.httpClient = *client
		c.httpClientSet = true
		return nil
	}
}

// WithAuthenticatedHTTPClient copies a client whose shared transport owns
// authentication. The caller owns idle-connection cleanup; the SDK does not add
// a Close method. The SDK leaves the Authorization header untouched. Use it
// alone; it cannot be combined with WithAPIKey or WithHTTPClient.
func WithAuthenticatedHTTPClient(client *http.Client) Option {
	return func(c *config) error {
		if client == nil {
			return errors.New("typesafe: WithAuthenticatedHTTPClient client is nil")
		}
		if c.authClientSet {
			return errors.New("typesafe: WithAuthenticatedHTTPClient configured more than once; use one WithAuthenticatedHTTPClient")
		}
		if c.apiKeySet {
			return errors.New("typesafe: WithAuthenticatedHTTPClient conflicts with WithAPIKey; choose one authentication strategy")
		}
		if c.httpClientSet {
			return errors.New("typesafe: WithAuthenticatedHTTPClient conflicts with WithHTTPClient; choose one client option")
		}
		c.httpClient = *client
		c.authClientSet = true
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

// WithAttemptTimeout bounds each HTTP attempt, including reading its body. It
// does not bound request preparation or response decoding outside the attempt.
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

// NewClient constructs a client without performing network access or reading
// credentials from the environment. Exactly one authentication strategy is
// required: WithAPIKey, optionally with WithHTTPClient, or
// WithAuthenticatedHTTPClient alone. WithHTTPClient alone does not authenticate.
func NewClient(opts ...Option) (*Client, error) {
	cfg := defaultConfig()
	for _, opt := range opts {
		if opt == nil {
			return nil, errors.New("typesafe: nil option")
		}
		if err := opt(&cfg); err != nil {
			return nil, err
		}
	}
	if cfg.apiKeySet == cfg.authClientSet {
		return nil, errors.New("typesafe: authentication is required; use WithAPIKey or WithAuthenticatedHTTPClient (WithHTTPClient only customizes API-key transport)")
	}
	if err := cfg.retry.validate(); err != nil {
		return nil, err
	}
	cfg.httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	u := *cfg.baseURL
	return &Client{apiKey: cfg.apiKey, baseURL: &u, httpClient: cfg.httpClient, defaultModel: cfg.defaultModel,
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
