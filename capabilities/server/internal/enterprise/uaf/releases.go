package uaf

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"sort"
	"strings"
	"sync"
)

//go:embed gen/*.json
var generated embed.FS

var (
	loadOnce sync.Once
	loaded   map[string]*Metamodel
	loadErr  error
)

func decodeGenerated(raw []byte) (*Metamodel, error) {
	var data GeneratedRelease
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&data); err != nil {
		return nil, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("generated UAF data has trailing content")
	}
	if data.FormatVersion != DataFormat || data.StandardVersion == "" || data.Metamodel.Version != data.StandardVersion ||
		len(data.Sources) != 2 || len(data.Metamodel.Stereotypes) == 0 || len(data.Metamodel.Enumerations) == 0 {
		return nil, fmt.Errorf("unsupported or incomplete generated UAF data")
	}
	return &data.Metamodel, nil
}

// Releases are the generated UAF releases embedded in this build, newest first.
// Missing or invalid generated data is an error; runtime never parses XMI.
func Releases() ([]*Metamodel, error) {
	loadOnce.Do(func() {
		loaded = map[string]*Metamodel{}
		entries, err := fs.ReadDir(generated, "gen")
		if err != nil {
			loadErr = err
			return
		}
		for _, entry := range entries {
			if !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			raw, err := generated.ReadFile("gen/" + entry.Name())
			if err != nil {
				loadErr = err
				return
			}
			model, err := decodeGenerated(raw)
			if err != nil {
				loadErr = fmt.Errorf("%s: %w", entry.Name(), err)
				return
			}
			if loaded[model.Version] != nil || entry.Name() != "uaf-"+model.Version+".json" {
				loadErr = fmt.Errorf("generated UAF release identity differs")
				return
			}
			loaded[model.Version] = model
		}
	})
	if loadErr != nil {
		return nil, loadErr
	}
	out := make([]*Metamodel, 0, len(loaded))
	for _, model := range loaded {
		out = append(out, model)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version > out[j].Version })
	return out, nil
}

// Current is the newest embedded release.
func Current() *Metamodel {
	releases, err := Releases()
	if err != nil || len(releases) == 0 {
		panic(fmt.Sprintf("uaf: no embedded release: %v", err))
	}
	return releases[0]
}

// Release is one embedded release by version ("1.3").
func Release(version string) (*Metamodel, bool) {
	if _, err := Releases(); err != nil {
		return nil, false
	}
	model, ok := loaded[version]
	return model, ok
}
