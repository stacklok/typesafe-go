package typesafe

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func systemOneResponseFromServer(t *testing.T, body string) (*SystemOneResponse, error) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, body)
	}))
	defer server.Close()
	client, err := NewClient(WithAPIKey("key"), WithBaseURL(server.URL), noRetry())
	if err != nil {
		t.Fatal(err)
	}
	return client.SystemOne(context.Background(), SystemOneRequest{
		State: "state", Model: "m", Questions: map[string]Question{"q": Noul(nil, nil)},
	})
}

func TestSystemOneUsageBoundaries(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	for _, usage := range []Usage{{}, {InputTokens: maxInt, OutputTokens: maxInt}} {
		body := fmt.Sprintf(`{"model":"m","answers":{"q":{"type":"noul","noul":0}},"usage":{"input_tokens":%d,"output_tokens":%d}}`, usage.InputTokens, usage.OutputTokens)
		response, err := systemOneResponseFromServer(t, body)
		if err != nil || response == nil || response.Usage != usage {
			t.Fatalf("usage %#v: response=%#v err=%v", usage, response, err)
		}
	}
}

func TestSystemOneProtocolErrorsRetainOnlyValidUsage(t *testing.T) {
	validUsage := `"usage":{"input_tokens":12,"output_tokens":34}`
	for name, body := range map[string]string{
		"wrong model type":    `{"model":{"private-response-marker":true},"answers":{},` + validUsage + `}`,
		"wrong answers shape": `{"model":"m","answers":[],` + validUsage + `}`,
		"malformed answer":    `{"model":"m","answers":{"q":{"type":7}},` + validUsage + `}`,
		"missing answer":      `{"model":"m","answers":{},` + validUsage + `}`,
	} {
		t.Run(name, func(t *testing.T) {
			response, err := systemOneResponseFromServer(t, body)
			var protocolErr *ProtocolError
			if response != nil || !errors.Is(err, ErrProtocol) || !errors.As(err, &protocolErr) {
				t.Fatalf("response=%#v err=%v", response, err)
			}
			if protocolErr.Usage == nil || *protocolErr.Usage != (Usage{InputTokens: 12, OutputTokens: 34}) {
				t.Fatalf("usage not retained: %#v", protocolErr.Usage)
			}
			if strings.Contains(fmt.Sprintf("%v %+v %#v", err, err, err), "private-response-marker") {
				t.Fatal("response payload leaked through error formatting")
			}
		})
	}

	t.Run("known-zero usage", func(t *testing.T) {
		response, err := systemOneResponseFromServer(t, `{"model":"m","answers":{},"usage":{"input_tokens":0,"output_tokens":0}}`)
		var protocolErr *ProtocolError
		if response != nil || !errors.Is(err, ErrProtocol) || !errors.As(err, &protocolErr) {
			t.Fatalf("response=%#v err=%v", response, err)
		}
		if protocolErr.Usage == nil || *protocolErr.Usage != (Usage{}) {
			t.Fatalf("known-zero usage not retained: %#v", protocolErr.Usage)
		}
	})

	valid := `{"model":"m","answers":{"q":{"type":"noul","noul":0}},"usage":%s}`
	for name, usage := range map[string]string{
		"null":         `null`,
		"missing":      `{}`,
		"partial":      `{"input_tokens":1}`,
		"negative in":  `{"input_tokens":-1,"output_tokens":2}`,
		"negative out": `{"input_tokens":1,"output_tokens":-2}`,
		"noninteger":   `{"input_tokens":1.5,"output_tokens":2}`,
		"overflow":     `{"input_tokens":9223372036854775808,"output_tokens":2}`,
	} {
		t.Run(name, func(t *testing.T) {
			response, err := systemOneResponseFromServer(t, fmt.Sprintf(valid, usage))
			var protocolErr *ProtocolError
			if response != nil || !errors.As(err, &protocolErr) || protocolErr.Usage != nil {
				t.Fatalf("response=%#v error=%#v", response, err)
			}
		})
	}
}

func TestSystemOneMalformedJSONNeverRetainsUsage(t *testing.T) {
	max := strconv.Itoa(int(^uint(0) >> 1))
	for name, body := range map[string]string{
		"duplicate outside usage":            `{"model":"m","model":"other","answers":{},"usage":{"input_tokens":1,"output_tokens":2}}`,
		"duplicate input inside usage":       `{"model":"m","answers":{},"usage":{"input_tokens":1,"input_tokens":2,"output_tokens":3}}`,
		"duplicate nested key outside usage": `{"model":"m","answers":{"q":{"type":"noul","noul":0,"noul":0}},"usage":{"input_tokens":1,"output_tokens":2}}`,
		"trailing JSON":                      `{"model":"m","answers":{},"usage":{"input_tokens":1,"output_tokens":2}} {}`,
		"incomplete JSON":                    `{"model":"m","answers":{},"usage":{"input_tokens":1,"output_tokens":` + max,
		"malformed JSON":                     `{"usage":{"input_tokens":1,"output_tokens":2},`,
	} {
		t.Run(name, func(t *testing.T) {
			response, err := systemOneResponseFromServer(t, body)
			var protocolErr *ProtocolError
			if response != nil || !errors.As(err, &protocolErr) || protocolErr.Usage != nil {
				t.Fatalf("response=%#v error=%#v", response, err)
			}
		})
	}
}
