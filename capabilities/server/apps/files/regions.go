package files

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"
	"unicode/utf8"
)

const (
	MaxImageRegions          = 64
	MaxImageRegionLabelBytes = 256
	MaxImageRegionIDBytes    = 128
)

// A rectangle refers to immutable original raster bytes, in normalized image coordinates.
type ImageRegion struct {
	ID     string  `json:"id"`
	Label  string  `json:"label"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}
type ImageRegionsRequest struct {
	Hash    string        `json:"hash"`
	Regions []ImageRegion `json:"regions"`
}

func imageRegionsPayload(raw []byte) (ImageRegionsRequest, error) {
	var p ImageRegionsRequest
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&p) != nil || d.Decode(new(any)) != io.EOF || p.Regions == nil || len(p.Regions) > MaxImageRegions {
		return p, fmt.Errorf("invalid image regions")
	}
	seen := map[string]bool{}
	for _, r := range p.Regions {
		if strings.TrimSpace(r.ID) == "" || len(r.ID) > MaxImageRegionIDBytes || !utf8.ValidString(r.ID) || seen[r.ID] || len(r.Label) > MaxImageRegionLabelBytes || !utf8.ValidString(r.Label) {
			return p, fmt.Errorf("invalid region identity or label")
		}
		seen[r.ID] = true
		for _, v := range []float64{r.X, r.Y, r.Width, r.Height} {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return p, fmt.Errorf("invalid region coordinate")
			}
		}
		if r.X < 0 || r.Y < 0 || r.Width <= 0 || r.Height <= 0 || r.X+r.Width > 1 || r.Y+r.Height > 1 {
			return p, fmt.Errorf("region exceeds original image bounds")
		}
	}
	return p, nil
}
func rasterAttachment(contentType string) bool {
	switch strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0])) {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return true
	}
	return false
}
