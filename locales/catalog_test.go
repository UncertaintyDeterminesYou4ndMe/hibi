package locales

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// A message missing from a translation shows in English, with no error.
func TestEveryLanguageHasEveryMessage(t *testing.T) {
	keys := func(file string) map[string]bool {
		contents, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var messages map[string]any
		if err := json.Unmarshal(contents, &messages); err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for k := range messages {
			if !strings.HasPrefix(k, "@") {
				out[k] = true
			}
		}
		return out
	}
	source := keys("en.json")
	for _, file := range []string{"zh-Hans.json"} {
		translated := keys(file)
		for k := range source {
			if !translated[k] {
				t.Errorf("%s has no %q", file, k)
			}
		}
		for k := range translated {
			if !source[k] {
				t.Errorf("%s has %q, which en.json does not", file, k)
			}
		}
	}
}
