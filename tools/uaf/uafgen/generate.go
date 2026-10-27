package uafgen

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"

	"platformserver/tools/uaf"
)

// Generate compiles every retained profile and identifies both original OMG
// files. Measurements remain intact source data; the public metamodel keeps
// the same profile-derived fields as the original runtime parser.
func Generate(source fs.FS) (map[string][]byte, error) {
	entries, err := fs.ReadDir(source, ".")
	if err != nil {
		return nil, err
	}
	output := map[string][]byte{}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "uaf-") || !strings.HasSuffix(name, "-profile.xmi") {
			continue
		}
		version := strings.TrimSuffix(strings.TrimPrefix(name, "uaf-"), "-profile.xmi")
		profile, err := fs.ReadFile(source, name)
		if err != nil {
			return nil, err
		}
		model, err := Parse(profile)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		model.Version = version
		data := uaf.GeneratedRelease{FormatVersion: uaf.DataFormat, StandardVersion: version, Metamodel: *model}
		for _, file := range []string{name, "uaf-" + version + "-measurements.xmi"} {
			raw, err := fs.ReadFile(source, file)
			if err != nil {
				return nil, err
			}
			digest := sha256.Sum256(raw)
			data.Sources = append(data.Sources, uaf.Source{File: file, SHA256: hex.EncodeToString(digest[:])})
		}
		raw, err := json.MarshalIndent(data, "", "  ")
		if err != nil {
			return nil, err
		}
		output["uaf-"+version+".json"] = append(raw, '\n')
	}
	if len(output) == 0 {
		return nil, fmt.Errorf("no UAF profiles found")
	}
	return output, nil
}
