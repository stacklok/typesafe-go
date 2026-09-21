package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode"

	typesafe "github.com/stacklok/typesafe-go"
)

const (
	modelName      = "jev-1.13.0"
	duplicateLabel = "duplicate_charge"
	teamLabel      = "team"
	urgencyLabel   = "urgency"
)

type result struct {
	Model string `json:"model"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	DuplicateCharge float64 `json:"duplicate_charge"`
	Team            struct {
		Choice        string             `json:"choice"`
		Probabilities map[string]float64 `json:"probabilities"`
		Confidence    float64            `json:"confidence"`
	} `json:"team"`
	Urgency struct {
		Score         float64            `json:"score"`
		Probabilities map[string]float64 `json:"probabilities"`
		Confidence    float64            `json:"confidence"`
	} `json:"urgency"`
}

func run(apiKeyFile string) (result, error) {
	key, err := os.ReadFile(apiKeyFile)
	if err != nil {
		return result{}, errors.New("could not read API key file")
	}
	apiKey := strings.TrimRightFunc(string(key), unicode.IsSpace)
	if strings.TrimSpace(apiKey) == "" {
		return result{}, errors.New("API key file is empty")
	}
	policy := typesafe.DefaultRetryPolicy()
	policy.MaxRetries = 0
	client, err := typesafe.NewClient(typesafe.WithAPIKey(apiKey), typesafe.WithRetryPolicy(policy))
	if err != nil {
		return result{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return evaluate(ctx, client)
}

func evaluate(ctx context.Context, client *typesafe.Client) (result, error) {
	response, err := client.SystemOne(ctx, typesafe.SystemOneRequest{
		State: map[string]string{"request": "I was charged twice for order A-104. Please help me get the duplicate charge refunded."},
		Model: modelName,
		Questions: map[string]typesafe.Question{
			duplicateLabel: typesafe.Noul("Does this request describe a duplicate charge?", nil),
			teamLabel: typesafe.Choice("Which team should handle this request?", map[string]typesafe.Content{
				"billing":   "Handles charges, refunds, invoices, and payment issues.",
				"technical": "Handles product, account access, and technical problems.",
				"other":     "Handles requests outside billing and technical support.",
			}),
			urgencyLabel: typesafe.Score("How urgent is this request?", []typesafe.Content{
				"Routine: the customer can wait.",
				"Needs prompt attention: respond soon.",
				"Time-sensitive blocking issue: immediate attention is needed.",
			}...),
		},
	})
	if err != nil {
		return result{}, err
	}
	duplicate, ok := response.Answers[duplicateLabel].(typesafe.NoulAnswer)
	if !ok {
		return result{}, fmt.Errorf("unexpected %s answer type", duplicateLabel)
	}
	team, ok := response.Answers[teamLabel].(typesafe.ChoiceAnswer)
	if !ok {
		return result{}, fmt.Errorf("unexpected %s answer type", teamLabel)
	}
	urgency, ok := response.Answers[urgencyLabel].(typesafe.ScoreAnswer)
	if !ok {
		return result{}, fmt.Errorf("unexpected %s answer type", urgencyLabel)
	}
	var out result
	out.Model = response.Model
	out.Usage.InputTokens = response.Usage.InputTokens
	out.Usage.OutputTokens = response.Usage.OutputTokens
	out.DuplicateCharge = duplicate.Noul
	out.Team.Choice = team.Choice
	out.Team.Probabilities = team.Probabilities
	out.Team.Confidence = team.Confidence
	out.Urgency.Score = urgency.Score
	out.Urgency.Probabilities = urgency.Probabilities
	out.Urgency.Confidence = urgency.Confidence
	return out, nil
}

func main() {
	apiKeyFile := flag.String("api-key-file", "", "path to a file containing the API key")
	flag.Parse()
	if *apiKeyFile == "" {
		fmt.Fprintln(os.Stderr, "live example: -api-key-file is required")
		os.Exit(1)
	}
	out, err := run(*apiKeyFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "live example:", err)
		os.Exit(1)
	}
	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		fmt.Fprintln(os.Stderr, "live example: could not write result")
		os.Exit(1)
	}
}
