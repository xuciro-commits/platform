// Command storage-files verifies a stopped deployment's entire file store.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"strings"

	"platformserver"
)

func main() {
	endpointEnv := flag.String("endpoint-env", "", "environment variable containing an S3 bucket URL or local directory")
	flag.Parse()
	address := os.Getenv(*endpointEnv)
	if address == "" {
		log.Fatal("a file store endpoint is required")
	}
	ctx := context.Background()
	var store platformserver.FileStore
	var err error
	if strings.HasPrefix(address, "http://") || strings.HasPrefix(address, "https://") {
		store, err = platformserver.OpenS3Files(ctx, address)
	} else {
		if info, statErr := os.Stat(address); statErr != nil || !info.IsDir() {
			log.Fatal("the local file store must already exist")
		}
		store, err = platformserver.NewLocalFiles(address)
	}
	if err != nil {
		log.Fatal(err)
	}
	manifest, err := platformserver.FileStoreManifest(ctx, store)
	if err != nil {
		log.Fatal(err)
	}
	if err := json.NewEncoder(os.Stdout).Encode(manifest); err != nil {
		log.Fatal(err)
	}
}
