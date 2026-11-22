package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStringifyMatchesJavaScript(t *testing.T) {
	input := `{"b":1,"2":[],"a":{},"1":[1.5,1e21,1e-7,-0,0.1,123456789012345680000,0.000001,2.5e-7],` +
		`"s":"q\"\\\b\f\n\r\t\u0001<>&é` + "\u2028" + `","n":null,"t":true,"x":{"y":[{"z":[]}]},"4294967295":0,"01":0}`
	want := "{\n  \"1\": [\n    1.5,\n    1e+21,\n    1e-7,\n    0,\n    0.1,\n    123456789012345680000,\n    0.000001,\n    2.5e-7\n  ],\n" +
		"  \"2\": [],\n  \"b\": 1,\n  \"a\": {},\n  \"s\": \"q\\\"\\\\\\b\\f\\n\\r\\t\\u0001<>&é\u2028\",\n  \"n\": null,\n  \"t\": true,\n" +
		"  \"x\": {\n    \"y\": [\n      {\n        \"z\": []\n      }\n    ]\n  },\n  \"4294967295\": 0,\n  \"01\": 0\n}"
	value, err := parseJSON([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if got := stringify(value); got != want {
		t.Fatalf("stringify differs from JSON.stringify(value, null, 2):\n%s\nwant\n%s", got, want)
	}
}

func TestCommittedPackagesMatchTheirSources(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..", "deploy", "packages")
	dirs, err := sources(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) < 8 {
		t.Fatalf("found %d package sources in %s", len(dirs), root)
	}
	for _, dir := range dirs {
		if _, err := check(dir); err != nil {
			t.Error(err)
		}
	}
}

func TestPackRefusesEscapingPaths(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("package.json", `{"id":"x","version":"0.1.0"}`)
	write("blueprint.json", `{"roots":[],"assets":[{"ref":{"app":"build","kind":"object","name":"x.a"},"path":"../a.json"}]}`)
	if _, err := pack(dir); err == nil {
		t.Fatal("an asset path outside the package must be refused")
	}
}
