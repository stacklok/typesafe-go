package typesafe

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SystemOneRequest contains shared state and questions for one inference call.
// Its values remain caller-owned and must not be mutated during SystemOne.
type SystemOneRequest struct {
	State     Content
	Model     string
	Questions map[string]Question
}

// SystemOne sends req once per configured attempt and validates every answer
// against the serialized outbound question shape.
func (c *Client) SystemOne(ctx context.Context, req SystemOneRequest) (*SystemOneResponse, error) {
	if ctx == nil {
		return nil, &ValidationError{Field: "context", Reason: "nil"}
	}
	model := req.Model
	if model == "" {
		model = c.defaultModel
	}
	if strings.TrimSpace(model) == "" {
		return nil, &ValidationError{Field: "model", Reason: "blank"}
	}
	if len(req.Questions) == 0 {
		return nil, &ValidationError{Field: "questions", Reason: "missing"}
	}
	for id, q := range req.Questions {
		if id == "" {
			return nil, &ValidationError{Field: "question_id", Reason: "blank"}
		}
		if q == nil || reflect.ValueOf(q).Kind() == reflect.Pointer && reflect.ValueOf(q).IsNil() {
			return nil, &ValidationError{Field: "question", Reason: "nil"}
		}
	}
	payload, err := json.Marshal(struct {
		State     Content             `json:"state"`
		Model     string              `json:"model"`
		Questions map[string]Question `json:"questions"`
	}{State: req.State, Model: model, Questions: req.Questions})
	if err != nil {
		return nil, &ValidationError{Field: "request", Reason: "invalid JSON"}
	}
	expected, err := snapshotRequestJSON(payload)
	if err != nil {
		return nil, &ValidationError{Field: "request", Reason: "invalid value"}
	}
	body, requestID, err := c.call(ctx, http.MethodPost, "/v1/systemone", payload)
	if err != nil {
		return nil, err
	}
	return decodeSystemOne(body, requestID, expected)
}

// ListModels returns currently advertised model cards and aliases.
func (c *Client) ListModels(ctx context.Context) (*ModelsResponse, error) {
	if ctx == nil {
		return nil, &ValidationError{Field: "context", Reason: "nil"}
	}
	body, requestID, err := c.call(ctx, http.MethodGet, "/v1/models", nil)
	if err != nil {
		return nil, err
	}
	return decodeModels(body, requestID)
}

func validateRequestJSON(data []byte) error {
	_, err := snapshotRequestJSON(data)
	return err
}

func snapshotRequestJSON(data []byte) (map[string]expectedAnswer, error) {
	if err := checkDuplicateKeys(data); err != nil {
		return nil, err
	}
	var root map[string]json.RawMessage
	if err := decodeOne(data, &root); err != nil {
		return nil, err
	}
	if !validJSONKind(root["state"], false) {
		return nil, errors.New("invalid state")
	}
	var questions map[string]json.RawMessage
	if err := json.Unmarshal(root["questions"], &questions); err != nil || len(questions) == 0 {
		return nil, errors.New("invalid questions")
	}
	expected := make(map[string]expectedAnswer, len(questions))
	for id, body := range questions {
		if id == "" {
			return nil, errors.New("invalid question id")
		}
		var q map[string]json.RawMessage
		if err := json.Unmarshal(body, &q); err != nil {
			return nil, err
		}
		var kind string
		if err := json.Unmarshal(q["type"], &kind); err != nil {
			return nil, err
		}
		if ins, ok := q["instructions"]; ok && string(ins) != "null" && !validJSONKind(ins, false) {
			return nil, errors.New("invalid instructions")
		}
		want := expectedAnswer{kind: kind}
		switch kind {
		case "noul":
			if raw, ok := q["criteria"]; ok {
				var values map[string]json.RawMessage
				if err := json.Unmarshal(raw, &values); err != nil {
					return nil, err
				}
				for _, name := range []string{"true", "false"} {
					value, ok := values[name]
					if !ok || string(value) != "null" && !validJSONKind(value, false) {
						return nil, errors.New("invalid criteria")
					}
				}
			}
		case "choice":
			var values map[string]json.RawMessage
			if err := json.Unmarshal(q["criteria"], &values); err != nil || values == nil {
				return nil, errors.New("invalid criteria")
			}
			want.choiceKeys = make(map[string]struct{}, len(values))
			for key, value := range values {
				if string(value) != "null" && !validJSONKind(value, false) {
					return nil, errors.New("invalid criteria")
				}
				want.choiceKeys[key] = struct{}{}
			}
		case "score":
			var values []json.RawMessage
			if err := json.Unmarshal(q["criteria"], &values); err != nil || len(values) == 0 {
				return nil, errors.New("invalid criteria")
			}
			for _, value := range values {
				if !validJSONKind(value, false) {
					return nil, errors.New("invalid criteria")
				}
			}
			want.scoreCount = len(values)
		default:
			return nil, errors.New("invalid question type")
		}
		expected[id] = want
	}
	return expected, nil
}

func validJSONKind(raw []byte, allowNull bool) bool {
	if len(raw) == 0 {
		return false
	}
	var v any
	if err := decodeOne(raw, &v); err != nil {
		return false
	}
	if v == nil {
		return allowNull
	}
	switch v.(type) {
	case string, map[string]any, []any:
		return true
	}
	return false
}

func (c *Client) call(ctx context.Context, method, path string, payload []byte) ([]byte, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", context.Cause(ctx)
	}
	totalCtx, cancel := context.WithTimeout(ctx, c.retry.TotalBudget)
	defer cancel()
	for attempt := 0; ; attempt++ {
		attemptCtx, stop := context.WithTimeout(totalCtx, c.attemptTimeout)
		body, id, headers, err, retryable := c.attempt(attemptCtx, method, path, payload, attempt)
		attemptCause := attemptCtx.Err()
		stop()
		if err == nil {
			return body, id, nil
		}
		if ctx.Err() != nil {
			return nil, id, context.Cause(ctx)
		}
		if attemptCause == context.DeadlineExceeded {
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				err = &AttemptTimeoutError{cause: context.DeadlineExceeded}
				retryable = c.retry.RetryTimeouts
			}
		}
		if totalCtx.Err() != nil {
			var apiErr *APIError
			if errors.As(err, &apiErr) {
				return nil, id, err
			}
			return nil, id, totalCtx.Err()
		}
		if !retryable || attempt >= c.retry.MaxRetries {
			return nil, id, err
		}
		delay, provided := retryAfter(headers, time.Now())
		if !provided {
			delay = c.retry.backoff(attempt)
		}
		if deadline, ok := totalCtx.Deadline(); ok && !time.Now().Add(delay).Before(deadline) {
			return nil, id, err
		}
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-totalCtx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			if ctx.Err() != nil {
				return nil, id, context.Cause(ctx)
			}
			return nil, id, totalCtx.Err()
		}
	}
}

func (c *Client) attempt(ctx context.Context, method, path string, payload []byte, attempt int) ([]byte, string, http.Header, error, bool) {
	u := *c.baseURL
	u.Path = path
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, "", nil, errors.New("typesafe: cannot create request"), false
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("X-Typesafe-Retry-Count", strconv.Itoa(attempt))
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, "", nil, ctx.Err(), true
		}
		wrapped := &ConnectionError{cause: err}
		return nil, "", nil, wrapped, c.retry.RetryConnectionErrors && !nonRetryableTransport(err)
	}
	var closeOnce sync.Once
	closeBody := func() { closeOnce.Do(func() { _ = resp.Body.Close() }) }
	stopClose := context.AfterFunc(ctx, closeBody)
	defer func() {
		stopClose()
		closeBody()
	}()
	requestID := sanitizeRequestID(resp.Header.Get("x-typesafe-request-id"), c.apiKey)
	reader := io.Reader(resp.Body)
	var gz *gzip.Reader
	if strings.EqualFold(resp.Header.Get("Content-Encoding"), "gzip") {
		gz, err = gzip.NewReader(resp.Body)
		if err != nil {
			return nil, requestID, resp.Header, &ConnectionError{cause: err}, c.retry.RetryConnectionErrors
		}
		defer gz.Close()
		reader = gz
	}
	data, err := readBounded(reader, c.responseLimit)
	if err != nil {
		var large *ResponseTooLargeError
		if errors.As(err, &large) {
			return nil, requestID, resp.Header, err, false
		}
		if ctx.Err() != nil {
			return nil, requestID, resp.Header, ctx.Err(), true
		}
		return nil, requestID, resp.Header, &ConnectionError{cause: err}, c.retry.RetryConnectionErrors
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		delay, _ := retryAfter(resp.Header, time.Now())
		apiErr := &APIError{StatusCode: resp.StatusCode, RequestID: requestID, RetryAfter: delay}
		retryable := resp.StatusCode == 408 || resp.StatusCode == 429 || resp.StatusCode >= 500
		return nil, requestID, resp.Header, apiErr, retryable
	}
	return data, requestID, resp.Header, nil, false
}

func sanitizeRequestID(id, apiKey string) string {
	if id == "" || len(id) > 128 || strings.Contains(id, apiKey) {
		return ""
	}
	for _, r := range id {
		if r < 0x21 || r > 0x7e {
			return ""
		}
	}
	return id
}

func readBounded(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, &ResponseTooLargeError{Limit: limit}
	}
	return data, nil
}
