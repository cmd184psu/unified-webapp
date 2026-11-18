package model

import "strings"

// ModelFamily names the family a model belongs to, from its name (phase H2):
// models of one family tend to share blind spots, so a reviewer from the
// worker's family is a weaker check. Tag and size are ignored; an unknown name
// is its own family.
func ModelFamily(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	if i := strings.LastIndex(n, "/"); i >= 0 {
		n = n[i+1:]
	}
	if i := strings.Index(n, ":"); i >= 0 {
		n = n[:i]
	}
	for _, f := range []struct{ prefix, family string }{
		{"qwen", "qwen"}, {"qwq", "qwen"}, {"mistral", "mistral"}, {"mixtral", "mistral"}, {"codestral", "mistral"}, {"devstral", "mistral"},
		{"gpt-oss", "gpt-oss"}, {"gpt", "openai"}, {"o1", "openai"}, {"o3", "openai"}, {"llama", "llama"}, {"deepseek", "deepseek"},
		{"gemma", "gemma"}, {"phi", "phi"}, {"kimi", "kimi"}, {"claude", "claude"}, {"glm", "glm"}, {"minimax", "minimax"},
		{"granite", "granite"}, {"command", "cohere"}, {"nemotron", "nemotron"},
	} {
		if strings.HasPrefix(n, f.prefix) {
			return f.family
		}
	}
	return n
}

// Review independence levels, from the best check to the worst.
const (
	IndependenceFamily    = "different_family"
	IndependenceModel     = "same_family"
	IndependenceSameModel = "same_model"
)

// ReviewIndependence says how independent a reviewer is of the worker whose
// work it checked; "" when either is unknown.
func ReviewIndependence(worker, reviewer string) string {
	switch {
	case worker == "" || reviewer == "":
		return ""
	case worker == reviewer:
		return IndependenceSameModel
	case ModelFamily(worker) == ModelFamily(reviewer):
		return IndependenceModel
	}
	return IndependenceFamily
}
