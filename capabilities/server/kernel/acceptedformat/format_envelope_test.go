package acceptedformat

import (
	"encoding/json"
	"platformserver/platform"
	"testing"
)

// These digests pin the original byte encoding, including distinctions that
// matter to retained results but disappear under float64 or nil normalization.
func TestDigestPreservesRetainedJSONEncoding(t *testing.T) {
	for _, test := range []struct {
		name, raw, digest string
	}{
		{"large integer and nested key order", ` {"z":9007199254740993,"a":{"b":2,"a":1}} `, "5b19723c04cc5d54a2c6be49f4fd02857e89d81ff8e648dd0ad2b2b082e40d87"},
		{"large integer and exponent spelling", `{"z":9007199254740993,"a":1e+03}`, "8a2857f393e5444b697189ca1290d81d3cb5b5c99754262e78a68e76e129d6be"},
		{"decimal spelling", `{"a":1.0}`, "c29a44abc114a1d75486434c013102c9e736f526b4fd19658d9492b7b224de6d"},
		{"integer spelling", `{"a":1}`, "015abd7f5cc57a2dd94b7590f04ad8084273905ee33ec5cebeae62276a97f862"},
		{"nil list", `{"items":null}`, "747831752f307e97f9283aa5687684e8277a439d38d55b45574aa9275e9489c5"},
		{"empty list", `{"items":[]}`, "eef46741adfc3a9f76294d3b78f37a45f113092ac9d44ee77c7a038a88ff09a1"},
		{"original HTML escaping", `"<&>"`, "4f625111c9a47ece97bd2ddbea3ecac60f880b03ac8ab17538d569b8728b53bc"},
	} {
		t.Run(test.name, func(t *testing.T) {
			digest, err := platform.CanonicalDigest(json.RawMessage(test.raw))
			if err != nil || digest != test.digest {
				t.Fatalf("retained digest changed: %s, %v", digest, err)
			}
		})
	}
	if digest, err := platform.CanonicalDigest(json.RawMessage(`{"broken":`)); err == nil || digest != "" {
		t.Fatalf("invalid raw image acquired an accepted digest: %q, %v", digest, err)
	}
}
