package typesafe

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type trackingBody struct {
	reader io.Reader
	closes atomic.Int32
	close  func()
}

func (b *trackingBody) Read(p []byte) (int, error) { return b.reader.Read(p) }
func (b *trackingBody) Close() error {
	b.closes.Add(1)
	if b.close != nil {
		b.close()
	}
	return nil
}

type interruptedReader struct{ sent bool }

func (r *interruptedReader) Read(p []byte) (int, error) {
	if !r.sent {
		r.sent = true
		copy(p, "{")
		return 1, nil
	}
	return 0, errors.New("synthetic interrupted read")
}

func gzipBytes(t *testing.T, data []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	writer := gzip.NewWriter(&out)
	if _, err := writer.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestPublicTransportClosesEveryResponseBodyExactlyOnce(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		headers http.Header
		reader  func(*testing.T) io.Reader
		limit   int64
		check   func(*testing.T, error)
	}{
		{"success", 200, nil, func(*testing.T) io.Reader { return bytes.NewBufferString(`{"models":[]}`) }, 1024, func(t *testing.T, err error) {
			if err != nil {
				t.Fatal(err)
			}
		}},
		{"API error", 422, nil, func(*testing.T) io.Reader { return bytes.NewBufferString(`{"private":"discarded"}`) }, 1024, func(t *testing.T, err error) {
			var api *APIError
			if !errors.Is(err, ErrAPI) || !errors.As(err, &api) || api.RequestID != "close-id" || api.StatusCode != 422 {
				t.Fatalf("API error metadata: %v", err)
			}
		}},
		{"protocol error", 200, nil, func(*testing.T) io.Reader { return bytes.NewBufferString(`{"models":null}`) }, 1024, func(t *testing.T, err error) {
			var protocolErr *ProtocolError
			if !errors.Is(err, ErrProtocol) || !errors.As(err, &protocolErr) || protocolErr.RequestID != "close-id" || protocolErr.Field != "models" {
				t.Fatalf("protocol metadata: %#v", err)
			}
		}},
		{"oversize", 200, nil, func(*testing.T) io.Reader { return bytes.NewBufferString(`{"models":[]}`) }, 4, func(t *testing.T, err error) {
			var large *ResponseTooLargeError
			if !errors.Is(err, ErrResponseTooLarge) || !errors.As(err, &large) || large.Limit != 4 {
				t.Fatalf("oversize metadata: %#v", err)
			}
		}},
		{"gzip", 200, http.Header{"Content-Encoding": {"gzip"}}, func(t *testing.T) io.Reader { return bytes.NewReader(gzipBytes(t, []byte(`{"models":[]}`))) }, 1024, func(t *testing.T, err error) {
			if err != nil {
				t.Fatal(err)
			}
		}},
		{"malformed gzip", 200, http.Header{"Content-Encoding": {"gzip"}}, func(*testing.T) io.Reader { return bytes.NewBufferString("not gzip") }, 1024, func(t *testing.T, err error) {
			var connection *ConnectionError
			if !errors.Is(err, ErrConnection) || !errors.As(err, &connection) {
				t.Fatalf("malformed gzip error: %v", err)
			}
		}},
		{"interrupted read", 200, nil, func(*testing.T) io.Reader { return &interruptedReader{} }, 1024, func(t *testing.T, err error) {
			var connection *ConnectionError
			if !errors.Is(err, ErrConnection) || !errors.As(err, &connection) {
				t.Fatalf("read error: %v", err)
			}
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			body := &trackingBody{reader: test.reader(t)}
			rt := roundTripFunc(func(*http.Request) (*http.Response, error) {
				headers := test.headers.Clone()
				if headers == nil {
					headers = make(http.Header)
				}
				headers.Set("x-typesafe-request-id", "close-id")
				return &http.Response{StatusCode: test.status, Header: headers, Body: body}, nil
			})
			client, err := NewClient(WithAPIKey("key"), WithHTTPClient(&http.Client{Transport: rt}), WithResponseLimit(test.limit), noRetry())
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.ListModels(context.Background())
			test.check(t, err)
			if body.closes.Load() != 1 {
				t.Fatalf("body closed %d times", body.closes.Load())
			}
		})
	}
}

type closeBlockingReader struct {
	started chan struct{}
	closed  chan struct{}
	once    sync.Once
}

func (r *closeBlockingReader) Read([]byte) (int, error) {
	r.once.Do(func() { close(r.started) })
	<-r.closed
	return 0, errors.New("unblocked by close")
}

func TestPublicTransportCancellationClosesBodyExactlyOnceAndReturnsCause(t *testing.T) {
	reader := &closeBlockingReader{started: make(chan struct{}), closed: make(chan struct{})}
	body := &trackingBody{reader: reader, close: func() {
		select {
		case <-reader.closed:
		default:
			close(reader.closed)
		}
	}}
	rt := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: body}, nil
	})
	policy := DefaultRetryPolicy()
	policy.MaxRetries = 0
	client, err := NewClient(WithAPIKey("key"), WithHTTPClient(&http.Client{Transport: rt}), WithAttemptTimeout(time.Minute), WithRetryPolicy(policy))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	cause := errors.New("synthetic caller cause")
	done := make(chan error, 1)
	go func() { _, err := client.ListModels(ctx); done <- err }()
	<-reader.started
	cancel(cause)
	if err := <-done; !errors.Is(err, cause) {
		t.Fatalf("caller cause lost: %v", err)
	}
	if body.closes.Load() != 1 {
		t.Fatalf("body closed %d times", body.closes.Load())
	}
}
