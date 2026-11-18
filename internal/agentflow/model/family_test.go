package model

import "testing"

func TestModelFamily(t *testing.T) {
	for name, want := range map[string]string{
		"qwen3.6:35b-a3b-mxfp8": "qwen", "qwen3-coder:30b": "qwen", "qwen2.5-coder:14b": "qwen",
		"mistral-small3.2:latest": "mistral", "gpt-oss:20b": "gpt-oss", "llama3.2:3b": "llama",
		"opencode/big-pickle": "big-pickle", "space-bunny-free": "space-bunny-free", "kimi-k3": "kimi",
		"moonshotai/kimi-k2.5": "kimi", "": "",
	} {
		if got := ModelFamily(name); got != want {
			t.Errorf("ModelFamily(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestReviewIndependence(t *testing.T) {
	for _, c := range []struct{ w, r, want string }{
		{"qwen3.6:35b", "qwen3-coder:30b", IndependenceModel},
		{"qwen3.6:35b", "mistral-small3.2", IndependenceFamily},
		{"qwen3.6:35b", "qwen3.6:35b", IndependenceSameModel},
		{"", "mistral", ""}, {"qwen3", "", ""},
	} {
		if got := ReviewIndependence(c.w, c.r); got != c.want {
			t.Errorf("ReviewIndependence(%q,%q) = %q, want %q", c.w, c.r, got, c.want)
		}
	}
}
