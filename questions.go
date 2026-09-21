package typesafe

import "encoding/json"

// Content is any value encodable as JSON.
type Content = any

// Question is one of NoulQuestion, ChoiceQuestion, or ScoreQuestion.
type Question interface{ question() }

// NoulCriteria optionally describes the true and false poles of a Noul question.
type NoulCriteria struct {
	True  Content
	False Content
}

// NoulQuestion asks for a scalar in the inclusive range [0,1].
type NoulQuestion struct {
	Instructions Content
	Criteria     *NoulCriteria
}

// ChoiceQuestion asks the model to select one named criterion.
type ChoiceQuestion struct {
	Instructions Content
	Criteria     map[string]Content
}

// ScoreQuestion asks for an expected zero-based index over ordered criteria.
type ScoreQuestion struct {
	Instructions Content
	Criteria     []Content
}

// Noul constructs a Noul question and copies criteria when non-nil.
func Noul(instructions Content, criteria *NoulCriteria) NoulQuestion {
	var copied *NoulCriteria
	if criteria != nil {
		v := *criteria
		copied = &v
	}
	return NoulQuestion{Instructions: instructions, Criteria: copied}
}

// Choice constructs a Choice question and copies the criteria map.
func Choice(instructions Content, criteria map[string]Content) ChoiceQuestion {
	copied := make(map[string]Content, len(criteria))
	for k, v := range criteria {
		copied[k] = v
	}
	return ChoiceQuestion{Instructions: instructions, Criteria: copied}
}

// Score constructs a Score question and copies the criteria slice.
func Score(instructions Content, criteria ...Content) ScoreQuestion {
	return ScoreQuestion{Instructions: instructions, Criteria: append([]Content(nil), criteria...)}
}

func (NoulQuestion) question()   {}
func (ChoiceQuestion) question() {}
func (ScoreQuestion) question()  {}

// MarshalJSON encodes q with the immutable noul discriminator.
func (q NoulQuestion) MarshalJSON() ([]byte, error) {
	type criteria struct {
		True  Content `json:"true"`
		False Content `json:"false"`
	}
	var c *criteria
	if q.Criteria != nil {
		c = &criteria{True: q.Criteria.True, False: q.Criteria.False}
	}
	return json.Marshal(struct {
		Type         string    `json:"type"`
		Instructions Content   `json:"instructions,omitempty"`
		Criteria     *criteria `json:"criteria,omitempty"`
	}{Type: "noul", Instructions: q.Instructions, Criteria: c})
}

// MarshalJSON encodes q with the immutable choice discriminator.
func (q ChoiceQuestion) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Type         string             `json:"type"`
		Instructions Content            `json:"instructions,omitempty"`
		Criteria     map[string]Content `json:"criteria"`
	}{Type: "choice", Instructions: q.Instructions, Criteria: q.Criteria})
}

// MarshalJSON encodes q with the immutable score discriminator.
func (q ScoreQuestion) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Type         string    `json:"type"`
		Instructions Content   `json:"instructions,omitempty"`
		Criteria     []Content `json:"criteria"`
	}{Type: "score", Instructions: q.Instructions, Criteria: q.Criteria})
}
