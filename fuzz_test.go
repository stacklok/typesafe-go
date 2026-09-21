package typesafe

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func FuzzDecodeSystemOneResponse(f *testing.F) {
	expected := map[string]expectedAnswer{
		"n": {kind: "noul"},
		"c": {kind: "choice", choiceKeys: map[string]struct{}{"a": {}, "b": {}}},
		"s": {kind: "score", scoreCount: 2},
	}
	f.Add([]byte(`{"model":"m","answers":{"n":{"type":"noul","noul":0},"c":{"type":"choice","choice":"a","probabilities":{"a":1,"b":0},"confidence":0},"s":{"type":"score","score":0,"probabilities":{"0":1,"1":0},"legend":{"0":{"large":1e400},"1":[true,null]},"confidence":0}},"usage":{"input_tokens":0,"output_tokens":0}}`))
	f.Add([]byte(`{"model":"m","model":"duplicate"}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		response, err := decodeSystemOne(data, "synthetic", expected)
		if err != nil {
			if !errors.Is(err, ErrProtocol) {
				t.Fatalf("decoder returned non-protocol error: %T %v", err, err)
			}
			var protocolErr *ProtocolError
			safeFields := map[string]bool{"response": true, "model": true, "answers": true, "usage": true}
			safeReasons := map[string]bool{"malformed JSON": true, "missing": true, "invalid": true, "malformed answer": true, "invalid noul": true, "wrong answer type": true, "invalid choice": true, "choice is not maximum probability": true, "out-of-set choice": true, "invalid probability keys": true, "invalid score": true, "invalid score range or keys": true, "unknown answer type": true, "missing requested answer": true}
			if !errors.As(err, &protocolErr) || !safeFields[protocolErr.Field] || !safeReasons[protocolErr.Reason] || protocolErr.Usage != nil && (protocolErr.Usage.InputTokens < 0 || protocolErr.Usage.OutputTokens < 0) || strings.Contains(fmt.Sprintf("%v %+v %#v", err, err, err), "private-response-marker") {
				t.Fatalf("unsafe protocol error: %#v", err)
			}
			return
		}
		if response.Model == "" || response.Answers == nil || len(response.Answers) < len(expected) || response.Usage.InputTokens < 0 || response.Usage.OutputTokens < 0 {
			t.Fatal("successful decode violated root invariants")
		}
		for id, want := range expected {
			answer, ok := response.Answers[id]
			if !ok || answer.AnswerType() != want.kind {
				t.Fatalf("requested answer %q missing or mismatched", id)
			}
		}
		for id, answer := range response.Answers {
			switch value := answer.(type) {
			case NoulAnswer:
				if !finiteRange(value.Noul, 0, 1) {
					t.Fatalf("%s invalid noul", id)
				}
			case ChoiceAnswer:
				selected, selectedPresent := value.Probabilities[value.Choice]
				if !finiteRange(value.Confidence, 0, 1) || value.Probabilities == nil || !selectedPresent {
					t.Fatalf("%s invalid choice", id)
				}
				for _, probability := range value.Probabilities {
					if !finiteRange(probability, 0, 1) || probability > selected {
						t.Fatalf("%s invalid probability", id)
					}
				}
			case ScoreAnswer:
				if !finite(value.Score) || !finiteRange(value.Confidence, 0, 1) || value.Probabilities == nil || value.Legend == nil || len(value.Probabilities) != len(value.Legend) {
					t.Fatalf("%s invalid score", id)
				}
				for key, probability := range value.Probabilities {
					if !finiteRange(probability, 0, 1) {
						t.Fatalf("%s invalid probability", id)
					}
					if legend, ok := value.Legend[key]; !ok || legend == nil {
						t.Fatalf("%s missing/non-null legend", id)
					}
				}
			default:
				t.Fatalf("%s unrecognized public answer %T", id, answer)
			}
		}
	})
}

func FuzzValidateRequestJSON(f *testing.F) {
	f.Add([]byte(`{"state":{"nested":[1e400,null,true]},"model":"m","questions":{"n":{"type":"noul","criteria":{"true":{"n":1e400},"false":null}},"c":{"type":"choice","instructions":[1e400],"criteria":{"a":{"large":123456789012345678901234567890}}},"s":{"type":"score","criteria":["a",[1e400]]}}}`))
	f.Add([]byte(`null`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if validateRequestJSON(data) != nil {
			return
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		var root map[string]any
		if err := decoder.Decode(&root); err != nil || root == nil {
			t.Fatalf("accepted request is not independently decodable: %v", err)
		}
		state, ok := root["state"]
		if !ok || state == nil {
			t.Fatal("accepted request has null/missing state")
		}
		switch state.(type) {
		case string, map[string]any, []any:
		default:
			t.Fatalf("accepted unsupported state %T", state)
		}
		questions, ok := root["questions"].(map[string]any)
		if !ok || len(questions) == 0 {
			t.Fatal("accepted request has no questions")
		}
		for id, raw := range questions {
			question, ok := raw.(map[string]any)
			if !ok || id == "" {
				t.Fatalf("invalid question %q", id)
			}
			kind, _ := question["type"].(string)
			switch kind {
			case "noul":
				if criteria, present := question["criteria"]; present {
					values, ok := criteria.(map[string]any)
					if !ok {
						t.Fatalf("noul criteria is %T", criteria)
					}
					if _, ok := values["true"]; !ok {
						t.Fatal("noul true missing")
					}
					if _, ok := values["false"]; !ok {
						t.Fatal("noul false missing")
					}
				}
			case "choice":
				if _, ok := question["criteria"].(map[string]any); !ok {
					t.Fatal("choice criteria is not object")
				}
			case "score":
				criteria, ok := question["criteria"].([]any)
				if !ok || len(criteria) < 1 {
					t.Fatal("score criteria missing")
				}
				for _, value := range criteria {
					if value == nil {
						t.Fatal("score criterion is null")
					}
				}
			default:
				t.Fatalf("accepted unknown type %q", kind)
			}
		}
	})
}
