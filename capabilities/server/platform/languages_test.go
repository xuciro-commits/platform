package platform

import (
	"fmt"
	"strings"
	"testing"
)

func TestApplicationLanguageBounds(t *testing.T) {
	for _, languages := range []Languages{nil, {"zh-CN": {"Entry": "项目"}}, {"fr": {}}} {
		if err := (Application{Languages: languages}).CheckLanguages(); err != nil {
			t.Fatal(err)
		}
	}
	tooManyLanguages := Languages{}
	for i := range 33 {
		tooManyLanguages[fmt.Sprintf("lang-%d", i)] = map[string]string{}
	}
	tooManyEntries := map[string]string{}
	for i := range 10001 {
		tooManyEntries[fmt.Sprintf("Entry %d", i)] = "项目"
	}
	for _, languages := range []Languages{
		{"": {"Entry": "项目"}}, {"zh/CN": {"Entry": "项目"}}, {"zh CN": {"Entry": "项目"}},
		{"zh-CN": {" ": "项目"}}, {"zh-CN": {"Entry": " "}},
		{"zh-CN": {strings.Repeat("x", 4097): "项目"}},
		{"zh-CN": {"Entry": strings.Repeat("x", 4097)}},
		tooManyLanguages, {"zh-CN": tooManyEntries},
	} {
		if err := (Application{Languages: languages}).CheckLanguages(); err == nil {
			t.Fatal("invalid application dictionary was accepted")
		}
	}
}
