package typesafe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const publicResponseFixture = `{"model":"m","answers":{"n":{"type":"noul","noul":0},"c":{"type":"choice","choice":"a","probabilities":{"a":1,"b":0},"confidence":0},"s":{"type":"score","score":0,"probabilities":{"0":1,"1":0},"legend":{"0":"low","1":{"nested":true}},"confidence":0}},"usage":{"input_tokens":0,"output_tokens":0}}`

func publicQuestions() map[string]Question {
	return map[string]Question{
		"n": Noul("decide", nil),
		"c": Choice("choose", map[string]Content{"a": "A", "b": "B"}),
		"s": Score("score", "low", "high"),
	}
}

func publicSystemOne(t *testing.T, body string) (*SystemOneResponse, error) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("x-typesafe-request-id", "qa-request")
		_, _ = io.WriteString(w, body)
	}))
	defer server.Close()
	client, err := NewClient(WithAPIKey("qa-secret"), WithBaseURL(server.URL), noRetry())
	if err != nil {
		t.Fatal(err)
	}
	return client.SystemOne(context.Background(), SystemOneRequest{State: "state", Model: "m", Questions: publicQuestions()})
}

func mutateResponse(t *testing.T, path []string, remove bool, value any) string {
	t.Helper()
	var root map[string]any
	if err := decodeOne([]byte(publicResponseFixture), &root); err != nil {
		t.Fatal(err)
	}
	object := root
	for _, part := range path[:len(path)-1] {
		next, ok := object[part].(map[string]any)
		if !ok {
			t.Fatalf("fixture path %v is not an object", path)
		}
		object = next
	}
	if remove {
		delete(object, path[len(path)-1])
	} else {
		object[path[len(path)-1]] = value
	}
	data, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func assertPublicProtocolError(t *testing.T, body string) {
	t.Helper()
	_, err := publicSystemOne(t, body)
	if !errors.Is(err, ErrProtocol) {
		t.Fatalf("wanted ErrProtocol, got %T %v", err, err)
	}
	var protocolErr *ProtocolError
	if !errors.As(err, &protocolErr) {
		t.Fatalf("wanted ProtocolError, got %T", err)
	}
	if protocolErr.RequestID != "qa-request" || protocolErr.Field == "" || protocolErr.Reason == "" {
		t.Fatalf("missing safe protocol metadata: %#v", protocolErr)
	}
	text := fmt.Sprintf("%v %+v %#v", err, err, err)
	wantText := "typesafe: invalid service response field " + safeField(protocolErr.Field)
	if err.Error() != wantText || strings.Contains(text, "qa-secret") || strings.Contains(text, "private-response-marker") {
		t.Fatalf("protocol error leaked data or was not static: %s", text)
	}
}

func TestPublicSystemOneRequiredFieldsMissingAndNull(t *testing.T) {
	paths := [][]string{
		{"model"}, {"answers"}, {"usage"}, {"usage", "input_tokens"}, {"usage", "output_tokens"},
		{"answers", "n", "type"}, {"answers", "n", "noul"},
		{"answers", "c", "type"}, {"answers", "c", "choice"}, {"answers", "c", "confidence"}, {"answers", "c", "probabilities"},
		{"answers", "s", "type"}, {"answers", "s", "score"}, {"answers", "s", "confidence"}, {"answers", "s", "probabilities"}, {"answers", "s", "legend"},
	}
	for _, path := range paths {
		name := strings.Join(path, "/")
		t.Run(name+"/missing", func(t *testing.T) {
			assertPublicProtocolError(t, mutateResponse(t, path, true, nil))
		})
		t.Run(name+"/null", func(t *testing.T) {
			assertPublicProtocolError(t, mutateResponse(t, path, false, nil))
		})
	}
}

func TestPublicSystemOneAdversarialResponseMatrix(t *testing.T) {
	cases := map[string]string{
		"missing requested answer": mutateResponse(t, []string{"answers", "n"}, true, nil),
		"wrong type":               mutateResponse(t, []string{"answers", "n", "type"}, false, "choice"),
		"malformed answer":         mutateResponse(t, []string{"answers", "n"}, false, "private-response-marker"),
		"unknown type":             mutateResponse(t, []string{"answers", "n", "type"}, false, "future"),
		"extra unknown answer":     strings.Replace(publicResponseFixture, `"answers":{`, `"answers":{"extra":{"type":"future"},`, 1),
		"duplicate JSON keys":      strings.Replace(publicResponseFixture, `"model":"m"`, `"model":"m","model":"private-response-marker"`, 1),
		"null probability":         mutateResponse(t, []string{"answers", "c", "probabilities", "a"}, false, nil),
		"probability above one":    mutateResponse(t, []string{"answers", "c", "probabilities", "a"}, false, json.Number("2")),
		"negative probability":     mutateResponse(t, []string{"answers", "c", "probabilities", "a"}, false, json.Number("-0.1")),
		"missing probability key":  mutateResponse(t, []string{"answers", "c", "probabilities", "b"}, true, nil),
		"extra probability key":    mutateResponse(t, []string{"answers", "c", "probabilities", "x"}, false, json.Number("0")),
		"choice outside set":       mutateResponse(t, []string{"answers", "c", "choice"}, false, "x"),
		"score out of range":       mutateResponse(t, []string{"answers", "s", "score"}, false, json.Number("2")),
		"null legend value":        mutateResponse(t, []string{"answers", "s", "legend", "0"}, false, nil),
		"empty body":               "",
		"trailing JSON":            publicResponseFixture + ` {}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) { assertPublicProtocolError(t, body) })
	}
}

func TestPublicChoiceArgmaxContract(t *testing.T) {
	accepted := map[string]struct {
		body   string
		choice string
	}{
		"tie selected a": {
			body:   strings.Replace(publicResponseFixture, `"choice":"a","probabilities":{"a":1,"b":0}`, `"choice":"a","probabilities":{"a":0.5,"b":0.5}`, 1),
			choice: "a",
		},
		"tie selected b": {
			body:   strings.Replace(publicResponseFixture, `"choice":"a","probabilities":{"a":1,"b":0}`, `"choice":"b","probabilities":{"a":0.5,"b":0.5}`, 1),
			choice: "b",
		},
		"small representable difference": {
			body:   strings.Replace(publicResponseFixture, `"choice":"a","probabilities":{"a":1,"b":0}`, `"choice":"b","probabilities":{"a":0.5,"b":0.5000000000000001}`, 1),
			choice: "b",
		},
	}
	for name, want := range accepted {
		t.Run(name, func(t *testing.T) {
			response, err := publicSystemOne(t, want.body)
			if err != nil {
				t.Fatalf("valid argmax rejected: %v", err)
			}
			if got := response.Answers["c"].(ChoiceAnswer).Choice; got != want.choice {
				t.Fatalf("choice = %q, want %q", got, want.choice)
			}
		})
	}

	for name, body := range map[string]string{
		"lower selected preserves usage": strings.Replace(strings.Replace(publicResponseFixture, `"choice":"a","probabilities":{"a":1,"b":0}`, `"choice":"a","probabilities":{"a":0.4,"b":0.6}`, 1), `"input_tokens":0,"output_tokens":0`, `"input_tokens":12,"output_tokens":34`, 1),
		"no epsilon":                     strings.Replace(publicResponseFixture, `"choice":"a","probabilities":{"a":1,"b":0}`, `"choice":"a","probabilities":{"a":0.5,"b":0.5000000000000001}`, 1),
		"extra selected absent":          strings.Replace(publicResponseFixture, `"answers":{`, `"answers":{"extra":{"type":"choice","choice":"x","probabilities":{"y":1},"confidence":0},`, 1),
		"extra lower selected":           strings.Replace(publicResponseFixture, `"answers":{`, `"answers":{"extra":{"type":"choice","choice":"x","probabilities":{"x":0.25,"y":0.75},"confidence":0},`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			response, err := publicSystemOne(t, body)
			if response != nil {
				t.Fatalf("protocol failure returned response: %#v", response)
			}
			var protocolErr *ProtocolError
			if !errors.Is(err, ErrProtocol) || !errors.As(err, &protocolErr) || protocolErr.RequestID != "qa-request" {
				t.Fatalf("wanted ProtocolError, got %#v", err)
			}
			if name == "lower selected preserves usage" && (protocolErr.Usage == nil || *protocolErr.Usage != (Usage{InputTokens: 12, OutputTokens: 34})) {
				t.Fatalf("wanted preserved usage, got %#v", err)
			}
		})
	}

	t.Run("extra known choice is checked intrinsically", func(t *testing.T) {
		extra := `"extra":{"type":"choice","choice":"x","probabilities":{"x":0.75,"y":0.25},"confidence":0},`
		response, err := publicSystemOne(t, strings.Replace(publicResponseFixture, `"answers":{`, `"answers":{`+extra, 1))
		if err != nil {
			t.Fatalf("valid extra choice rejected: %v", err)
		}
		if got := response.Answers["extra"].(ChoiceAnswer).Choice; got != "x" {
			t.Fatalf("extra choice = %q, want x", got)
		}
	})
}

func TestPublicChoiceAllowsEmptyLabel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"model":"m","answers":{"q":{"type":"choice","choice":"","probabilities":{"":1,"other":0},"confidence":1}},"usage":{"input_tokens":0,"output_tokens":0}}`)
	}))
	defer server.Close()
	client, err := NewClient(WithAPIKey("key"), WithBaseURL(server.URL), noRetry())
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.SystemOne(context.Background(), SystemOneRequest{State: "state", Model: "m", Questions: map[string]Question{
		"q": Choice("choose", map[string]Content{"": "empty label", "other": "other label"}),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if response.Answers["q"].(ChoiceAnswer).Choice != "" {
		t.Fatal("empty selected label was rewritten")
	}
}

func TestPublicSystemOneBaselineZerosAndAdditiveFields(t *testing.T) {
	body := strings.Replace(publicResponseFixture, `"model":"m"`, `"model":"m","future_root":{"private-response-marker":true}`, 1)
	body = strings.Replace(body, `"type":"choice"`, `"type":"choice","future_answer":[1,null]`, 1)
	response, err := publicSystemOne(t, body)
	if err != nil {
		t.Fatal(err)
	}
	if response.Model != "m" || response.Usage != (Usage{}) || response.Answers["n"].(NoulAnswer).Noul != 0 || response.Answers["c"].(ChoiceAnswer).Confidence != 0 {
		t.Fatalf("zero or additive-field contract failed: %#v", response)
	}
	score := response.Answers["s"].(ScoreAnswer)
	if score.Score != 0 || score.Confidence != 0 {
		t.Fatalf("score zeros changed: %#v", score)
	}
}

func TestPublicSystemOneAcceptsNestedArbitraryJSONNumbers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("x-typesafe-request-id", "numbers")
		_, _ = io.WriteString(w, `{"model":"m","answers":{"n":{"type":"noul","noul":0},"c":{"type":"choice","choice":"a","probabilities":{"a":1},"confidence":0},"s":{"type":"score","score":0,"probabilities":{"0":1},"legend":{"0":{"exponent":1e400,"integer":123456789012345678901234567890}},"confidence":0}},"usage":{"input_tokens":0,"output_tokens":0}}`)
	}))
	defer server.Close()
	client, err := NewClient(WithAPIKey("key"), WithBaseURL(server.URL), noRetry())
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.SystemOne(context.Background(), SystemOneRequest{
		State: json.RawMessage(`{"nested":[1e400,123456789012345678901234567890]}`), Model: "m",
		Questions: map[string]Question{
			"n": Noul(json.RawMessage(`{"nested":1e400}`), nil),
			"c": Choice(json.RawMessage(`[1e400]`), map[string]Content{"a": json.RawMessage(`{"n":1e400}`)}),
			"s": Score(json.RawMessage(`{"n":1e400}`), json.RawMessage(`[1e400]`)),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	legend := response.Answers["s"].(ScoreAnswer).Legend["0"].(map[string]any)
	if legend["exponent"].(json.Number).String() != "1e400" || legend["integer"].(json.Number).String() != "123456789012345678901234567890" {
		t.Fatalf("legend numbers changed: %#v", legend)
	}
}
