package analyze

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// AI is Claude's interpretation. It is added next to the rule-based result, never replacing it.
type AI struct {
	Model    string `json:"model"`
	Summary  string `json:"summary"`
	Purposes []Item `json:"purposes"`
	Sectors  []Item `json:"sectors"`
	Repos    []Note `json:"repo_notes"`
	In       int64  `json:"input_tokens"`
	Out      int64  `json:"output_tokens"`
}

type Item struct {
	Name       string `json:"name"`
	Confidence string `json:"confidence"`
	Evidence   string `json:"evidence"`
}

type Note struct {
	Repo    string `json:"repo"`
	Purpose string `json:"purpose"`
	Sector  string `json:"sector"`
}

var itemSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"name":       map[string]any{"type": "string"},
		"confidence": map[string]any{"type": "string", "enum": []string{"high", "medium", "low"}},
		"evidence":   map[string]any{"type": "string"},
	},
	"required":             []string{"name", "confidence", "evidence"},
	"additionalProperties": false,
}

var schema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"summary":  map[string]any{"type": "string"},
		"purposes": map[string]any{"type": "array", "items": itemSchema},
		"sectors":  map[string]any{"type": "array", "items": itemSchema},
		"repo_notes": map[string]any{"type": "array", "items": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"repo":    map[string]any{"type": "string"},
				"purpose": map[string]any{"type": "string"},
				"sector":  map[string]any{"type": "string"},
			},
			"required":             []string{"repo", "purpose", "sector"},
			"additionalProperties": false,
		}},
	},
	"required":             []string{"summary", "purposes", "sectors", "repo_notes"},
	"additionalProperties": false,
}

const prompt = `You are a software skills analyst. You will receive a digest built from the public repositories of a GitHub profile: languages, dependencies, topics, descriptions and the start of each README. A rule-based pre-analysis is included too.

Your task:
1. summary: 4-6 sentences describing which languages the developer uses, for what purpose, and which industries they lean toward.
2. purposes: usage purposes (e.g. "Embedded / IoT", "Web Backend"), strongest first.
3. sectors: industries the projects serve. Only list sectors with real evidence in the data; mark weak evidence as "low" confidence. Use "General / Personal Project" for personal or learning projects.
4. repo_notes: a one-line purpose and sector for each repo.

In evidence, briefly name the repo, dependency or word it is based on. Do not invent anything that is not in the data. Write all text in English, even if the READMEs are in another language.`

type digestRepo struct {
	Name     string   `json:"name"`
	Desc     string   `json:"description,omitempty"`
	Topics   []string `json:"topics,omitempty"`
	Langs    []string `json:"languages"`
	Deps     []string `json:"key_dependencies,omitempty"`
	Purposes []string `json:"rule_purposes,omitempty"`
	Sectors  []string `json:"rule_sectors,omitempty"`
	Stars    int      `json:"stars"`
	Pushed   string   `json:"last_push"`
	Readme   string   `json:"readme_excerpt,omitempty"`
}

func digest(p *Profile) string {
	var repos []digestRepo
	for _, r := range p.Repos {
		d := digestRepo{
			Name: r.Name, Desc: r.Desc, Topics: r.Topics, Deps: r.Deps,
			Stars: r.Stars, Pushed: r.Pushed.Format("2006-01"),
			Readme: strings.TrimSpace(r.Head),
		}
		for i, l := range r.Langs {
			if i >= 4 {
				break
			}
			d.Langs = append(d.Langs, fmt.Sprintf("%s %.0f%%", l.Name, l.Pct))
		}
		for _, s := range r.Purposes {
			d.Purposes = append(d.Purposes, s.Name)
		}
		for _, s := range r.Sectors {
			d.Sectors = append(d.Sectors, s.Name)
		}
		repos = append(repos, d)
	}
	var langs []string
	for i, l := range p.Langs {
		if i >= 10 {
			break
		}
		langs = append(langs, fmt.Sprintf("%s %.1f%%", l.Name, l.Weight))
	}
	u := p.User
	payload := map[string]any{
		"user":               map[string]any{"login": u.Login, "name": u.Name, "bio": u.Bio, "company": u.Company},
		"weighted_languages": langs,
		"repos":              repos,
	}
	b, _ := json.MarshalIndent(payload, "", " ")
	return string(b)
}

// Ask sends the profile digest to Claude and returns a structured interpretation.
// Only public repo data is sent; the GitHub token is never sent.
func Ask(ctx context.Context, key, model string, p *Profile) (*AI, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, errors.New("no Anthropic API key")
	}
	if model == "" {
		model = "claude-opus-5-5"
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	client := anthropic.NewClient(option.WithAPIKey(key))
	resp, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: 16000,
		System:    []anthropic.TextBlockParam{{Text: prompt}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("Profile data:\n\n" + digest(p))),
		},
		OutputConfig: anthropic.OutputConfigParam{
			Effort: anthropic.OutputConfigEffortLow,
			Format: anthropic.JSONOutputFormatParam{Schema: schema},
		},
	})
	if err != nil {
		var apiErr *anthropic.Error
		if errors.As(err, &apiErr) {
			switch apiErr.StatusCode {
			case 401:
				return nil, errors.New("invalid Anthropic API key")
			case 429:
				return nil, errors.New("Anthropic rate limit, try again later")
			}
			return nil, fmt.Errorf("Anthropic API error (HTTP %d)", apiErr.StatusCode)
		}
		return nil, fmt.Errorf("cannot reach the Anthropic API: %w", err)
	}
	if resp.StopReason == anthropic.StopReasonRefusal {
		return nil, errors.New("the model declined this request")
	}
	var out string
	for _, block := range resp.Content {
		if tb, ok := block.AsAny().(anthropic.TextBlock); ok {
			out += tb.Text
		}
	}
	var ai AI
	if err := json.Unmarshal([]byte(out), &ai); err != nil {
		return nil, fmt.Errorf("cannot parse the model reply: %w", err)
	}
	ai.Model = model
	ai.In = resp.Usage.InputTokens
	ai.Out = resp.Usage.OutputTokens
	return &ai, nil
}
