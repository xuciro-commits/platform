package platform

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

const (
	MediaSetType         = "build.mediaset"
	MediaSetVersionType  = "build.mediasetversion"
	SchemaMediaSetAdd    = "build.mediaset.add"
	SchemaMediaSetRemove = "build.mediaset.remove"
)

type MediaSet struct {
	Record
	Name              string      `json:"name" field:"required,search"`
	Title             string      `json:"title" field:"required,search"`
	DefinitionVersion int         `json:"definitionVersion,omitempty" field:"readonly"`
	Published         string      `json:"published,omitempty" field:"readonly" type:"longtext"`
	Versions          []string    `json:"definitionVersions,omitempty" field:"readonly" type:"json"`
	Description       string      `json:"description,omitempty" type:"longtext"`
	Version           int         `json:"version" field:"readonly"`
	Items             []MediaItem `json:"items" field:"readonly" type:"json"`
}

type MediaItem struct {
	Hash        string    `json:"hash"`
	Name        string    `json:"name"`
	ContentType string    `json:"contentType"`
	Size        int       `json:"size"`
	By          string    `json:"by"`
	At          time.Time `json:"at"`
}

type MediaSetVersion struct {
	Record
	MediaSet string      `json:"mediaSet" field:"required" ref:"build.mediaset"`
	Version  int         `json:"version" field:"required"`
	Items    []MediaItem `json:"items" type:"json"`
	By       string      `json:"by"`
	At       time.Time   `json:"at"`
	Request  string      `json:"requestKey,omitempty" field:"readonly"`
}

func MediaSetVersionID(id string, version int) string { return fmt.Sprintf("%s@%d", id, version) }

func ValidMediaHash(hash string) bool {
	decoded, err := hex.DecodeString(hash)
	return err == nil && len(decoded) == 32 && hex.EncodeToString(decoded) == hash
}

type Attachment struct {
	MediaSet string `json:"mediaSet"`
	Item     string `json:"item"`
}

func RetainedMediaVersions(set MediaSet) []MediaSetVersion {
	var asset struct {
		Body json.RawMessage `json:"body"`
	}
	var definition struct {
		History []MediaSetVersion `json:"history"`
	}
	if set.Published != "" {
		_ = json.Unmarshal([]byte(set.Published), &asset)
		_ = json.Unmarshal(asset.Body, &definition)
	}
	return definition.History
}
