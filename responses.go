package typesafe

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strconv"
	"time"
)

// Answer is a validated NoulAnswer, ChoiceAnswer, or ScoreAnswer.
type Answer interface {
	AnswerType() string
	answer()
}

// NoulAnswer contains the model's scalar Noul result.
type NoulAnswer struct{ Noul float64 }

// AnswerType returns "noul".
func (NoulAnswer) AnswerType() string { return "noul" }
func (NoulAnswer) answer()            {}

// ChoiceAnswer contains the selected label and server-provided statistics.
type ChoiceAnswer struct {
	Choice        string
	Probabilities map[string]float64
	Confidence    float64
}

// AnswerType returns "choice".
func (ChoiceAnswer) AnswerType() string { return "choice" }
func (ChoiceAnswer) answer()            {}

// ScoreAnswer contains the expected index and server-provided distribution and legend.
type ScoreAnswer struct {
	Score         float64
	Probabilities map[string]float64
	Legend        map[string]Content
	Confidence    float64
}

// AnswerType returns "score".
func (ScoreAnswer) AnswerType() string { return "score" }
func (ScoreAnswer) answer()            {}

// Usage reports API token accounting.
type Usage struct{ InputTokens, OutputTokens int }

// SystemOneResponse is a validated System One response.
type SystemOneResponse struct {
	Model     string
	Answers   map[string]Answer
	Usage     Usage
	RequestID string
}

// Model describes an advertised model or alias.
type Model struct{ Name, Description, ReleaseDate string }

// ModelsResponse contains advertised models and response metadata.
type ModelsResponse struct {
	Models    []Model
	RequestID string
}

type rawResponse struct {
	Model   *string                     `json:"model"`
	Answers *map[string]json.RawMessage `json:"answers"`
	Usage   *struct {
		Input  *int `json:"input_tokens"`
		Output *int `json:"output_tokens"`
	} `json:"usage"`
}

type rawAnswer struct {
	Type          *string              `json:"type"`
	Noul          *float64             `json:"noul"`
	Choice        *string              `json:"choice"`
	Score         *float64             `json:"score"`
	Probabilities *map[string]*float64 `json:"probabilities"`
	Legend        *map[string]Content  `json:"legend"`
	Confidence    *float64             `json:"confidence"`
}

type expectedAnswer struct {
	kind       string
	choiceKeys map[string]struct{}
	scoreCount int
}

func decodeSystemOne(data []byte, requestID string, expected map[string]expectedAnswer) (*SystemOneResponse, error) {
	if err := checkDuplicateKeys(data); err != nil {
		return nil, protocol(requestID, "response", "malformed JSON")
	}
	var envelope map[string]json.RawMessage
	if err := decodeOne(data, &envelope); err != nil {
		return nil, protocol(requestID, "response", "malformed JSON")
	}
	usage := decodeUsage(envelope["usage"])
	invalid := func(field, reason string) error {
		return &ProtocolError{RequestID: requestID, Field: field, Reason: reason, Usage: usage}
	}
	var raw rawResponse
	if err := decodeOne(data, &raw); err != nil {
		return nil, invalid("response", "malformed JSON")
	}
	if raw.Model == nil || *raw.Model == "" {
		return nil, invalid("model", "missing")
	}
	if raw.Answers == nil {
		return nil, invalid("answers", "missing")
	}
	if raw.Usage == nil || raw.Usage.Input == nil || raw.Usage.Output == nil {
		return nil, invalid("usage", "missing")
	}
	if *raw.Usage.Input < 0 || *raw.Usage.Output < 0 {
		return nil, invalid("usage", "invalid")
	}
	answers := make(map[string]Answer, len(*raw.Answers))
	for id, body := range *raw.Answers {
		var a rawAnswer
		if err := decodeOne(body, &a); err != nil || a.Type == nil {
			return nil, invalid("answers", "malformed answer")
		}
		want, requested := expected[id]
		var out Answer
		switch *a.Type {
		case "noul":
			if a.Noul == nil || !finiteRange(*a.Noul, 0, 1) {
				return nil, invalid("answers", "invalid noul")
			}
			if requested && want.kind != "noul" {
				return nil, invalid("answers", "wrong answer type")
			}
			out = NoulAnswer{Noul: *a.Noul}
		case "choice":
			probabilities, ok := numericMap(a.Probabilities)
			if a.Choice == nil || !ok || a.Confidence == nil || !finiteRange(*a.Confidence, 0, 1) {
				return nil, invalid("answers", "invalid choice")
			}
			if !choiceIsArgmax(*a.Choice, probabilities) {
				return nil, invalid("answers", "choice is not maximum probability")
			}
			if requested {
				if want.kind != "choice" {
					return nil, invalid("answers", "wrong answer type")
				}
				if _, ok := want.choiceKeys[*a.Choice]; !ok {
					return nil, invalid("answers", "out-of-set choice")
				}
				if !sameKeys(probabilities, want.choiceKeys) {
					return nil, invalid("answers", "invalid probability keys")
				}
			}
			out = ChoiceAnswer{Choice: *a.Choice, Probabilities: probabilities, Confidence: *a.Confidence}
		case "score":
			probabilities, ok := numericMap(a.Probabilities)
			if a.Score == nil || !ok || a.Legend == nil || a.Confidence == nil || !finite(*a.Score) || !finiteRange(*a.Confidence, 0, 1) || !validLegend(*a.Legend) || !sameMapKeys(probabilities, *a.Legend) || !scoreKeys(probabilities, len(probabilities)) {
				return nil, invalid("answers", "invalid score")
			}
			if requested {
				if want.kind != "score" {
					return nil, invalid("answers", "wrong answer type")
				}
				if *a.Score < 0 || *a.Score > float64(want.scoreCount-1) || !scoreKeys(probabilities, want.scoreCount) {
					return nil, invalid("answers", "invalid score range or keys")
				}
			}
			out = ScoreAnswer{Score: *a.Score, Probabilities: probabilities, Legend: *a.Legend, Confidence: *a.Confidence}
		default:
			return nil, invalid("answers", "unknown answer type")
		}
		answers[id] = out
	}
	for id := range expected {
		if _, ok := answers[id]; !ok {
			return nil, invalid("answers", "missing requested answer")
		}
	}
	return &SystemOneResponse{Model: *raw.Model, Answers: answers, Usage: Usage{*raw.Usage.Input, *raw.Usage.Output}, RequestID: requestID}, nil
}

func decodeModels(data []byte, requestID string) (*ModelsResponse, error) {
	if err := checkDuplicateKeys(data); err != nil {
		return nil, protocol(requestID, "response", "malformed JSON")
	}
	var raw struct {
		Models *[]struct {
			Name, Description string
			ReleaseDate       string `json:"release_date"`
		} `json:"models"`
	}
	if err := decodeOne(data, &raw); err != nil || raw.Models == nil {
		return nil, protocol(requestID, "models", "invalid")
	}
	models := make([]Model, len(*raw.Models))
	for i, m := range *raw.Models {
		if m.Name == "" || m.Description == "" || !validDate(m.ReleaseDate) {
			return nil, protocol(requestID, "models", "invalid model card")
		}
		models[i] = Model{Name: m.Name, Description: m.Description, ReleaseDate: m.ReleaseDate}
	}
	return &ModelsResponse{Models: models, RequestID: requestID}, nil
}

func protocol(id, field, reason string) error {
	return &ProtocolError{RequestID: id, Field: field, Reason: reason}
}

func decodeUsage(data []byte) *Usage {
	var raw struct {
		Input  *int `json:"input_tokens"`
		Output *int `json:"output_tokens"`
	}
	if len(data) == 0 || decodeOne(data, &raw) != nil || raw.Input == nil || raw.Output == nil || *raw.Input < 0 || *raw.Output < 0 {
		return nil
	}
	return &Usage{InputTokens: *raw.Input, OutputTokens: *raw.Output}
}

func numericMap(raw *map[string]*float64) (map[string]float64, bool) {
	if raw == nil {
		return nil, false
	}
	out := make(map[string]float64, len(*raw))
	for key, value := range *raw {
		if value == nil || !finiteRange(*value, 0, 1) {
			return nil, false
		}
		out[key] = *value
	}
	return out, true
}

func choiceIsArgmax(choice string, probabilities map[string]float64) bool {
	selected, ok := probabilities[choice]
	if !ok {
		return false
	}
	for _, probability := range probabilities {
		if probability > selected {
			return false
		}
	}
	return true
}

func sameKeys(values map[string]float64, keys map[string]struct{}) bool {
	if len(values) != len(keys) {
		return false
	}
	for key := range values {
		if _, ok := keys[key]; !ok {
			return false
		}
	}
	return true
}

func sameMapKeys(left map[string]float64, right map[string]Content) bool {
	if len(left) != len(right) {
		return false
	}
	for key := range left {
		if _, ok := right[key]; !ok {
			return false
		}
	}
	return true
}

func scoreKeys(values map[string]float64, count int) bool {
	if len(values) != count {
		return false
	}
	for key := range values {
		n, err := strconv.Atoi(key)
		if err != nil || n < 0 || n >= count || strconv.Itoa(n) != key {
			return false
		}
	}
	return true
}

func validLegend(legend map[string]Content) bool {
	for _, value := range legend {
		raw, err := json.Marshal(value)
		if err != nil || !validJSONKind(raw, false) {
			return false
		}
	}
	return true
}

func finite(v float64) bool                { return !math.IsNaN(v) && !math.IsInf(v, 0) }
func finiteRange(v, min, max float64) bool { return finite(v) && v >= min && v <= max }
func validDate(s string) bool {
	if len(s) != 10 {
		return false
	}
	t, err := time.Parse("2006-01-02", s)
	return err == nil && t.Format("2006-01-02") == s
}
func decodeOne(data []byte, dst any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err := d.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return errors.New("multiple JSON values")
	}
	return nil
}

// checkDuplicateKeys rejects ambiguous JSON objects at the protocol boundary.
func checkDuplicateKeys(data []byte) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var walk func() error
	walk = func() error {
		t, err := d.Token()
		if err != nil {
			return err
		}
		delim, ok := t.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]struct{}{}
			for d.More() {
				k, err := d.Token()
				if err != nil {
					return err
				}
				key, ok := k.(string)
				if !ok {
					return errors.New("invalid object key")
				}
				if _, ok := seen[key]; ok {
					return errors.New("duplicate JSON key")
				}
				seen[key] = struct{}{}
				if err := walk(); err != nil {
					return err
				}
			}
			_, err = d.Token()
			return err
		case '[':
			for d.More() {
				if err := walk(); err != nil {
					return err
				}
			}
			_, err = d.Token()
			return err
		default:
			return errors.New("unexpected JSON delimiter")
		}
	}
	if err := walk(); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New("multiple JSON values")
	}
	return nil
}
