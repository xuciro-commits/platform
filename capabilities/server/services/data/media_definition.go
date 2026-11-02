package data

import (
	"crypto/sha256"
	"fmt"
	"platformserver/platform"
)

func CheckMediaDefinition(items []platform.MediaItem, history []platform.MediaSetVersion, blobs map[string][]byte) error {
	all := append([]platform.MediaItem{}, items...)
	for _, version := range history {
		all = append(all, version.Items...)
	}
	for _, item := range all {
		raw, ok := blobs[item.Hash]
		if !ok || len(raw) != item.Size || fmt.Sprintf("%x", sha256.Sum256(raw)) != item.Hash {
			return fmt.Errorf("Media content differs from its retained item")
		}
	}
	return nil
}
