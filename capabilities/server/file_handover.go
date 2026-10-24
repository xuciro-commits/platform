package platformserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
)

// FileStoreManifest verifies actual bytes, not object-store ETags or timestamps.
// The deployment owner must stop all writers to these prefixes before calling.
func FileStoreManifest(ctx context.Context, store FileStore) (map[string]string, error) {
	inventory, ok := store.(FileInventory)
	if !ok {
		return nil, fmt.Errorf("file store does not support a complete inventory")
	}
	result := map[string]string{}
	after := ""
	for {
		page, err := inventory.Inventory(ctx, "", after, 1024)
		if err != nil {
			return nil, err
		}
		previous := after
		for _, file := range page.Files {
			if file.Key <= previous {
				return nil, fmt.Errorf("file inventory is not strictly ordered")
			}
			body, size, err := store.Get(ctx, file.Key)
			if err != nil {
				return nil, err
			}
			hash := sha256.New()
			count, readErr := io.Copy(hash, body)
			closeErr := body.Close()
			if readErr != nil {
				return nil, readErr
			}
			if closeErr != nil {
				return nil, closeErr
			}
			if count != size || count != file.Size {
				return nil, fmt.Errorf("file %s changed during handover", file.Key)
			}
			result[file.Key] = hex.EncodeToString(hash.Sum(nil))
			previous = file.Key
		}
		if page.Next == "" {
			return result, nil
		}
		if page.Next != previous || page.Next <= after {
			return nil, fmt.Errorf("file inventory cannot prove complete traversal")
		}
		after = page.Next
	}
}
