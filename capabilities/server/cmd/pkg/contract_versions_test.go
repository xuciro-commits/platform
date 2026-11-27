package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"platformserver/kernel/protocol"
	"platformserver/platform"
)

func TestK51CompilerRetainsDistinctContractMajors(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("package.yaml", "id: stock\nversion: 1.0.0\ntitle: Stock\nnamespace: stock\nroots:\n  - {app: build, kind: contract, name: stock.post}\n  - {app: build, kind: contract, name: stock.post@2}\n")
	write("contracts/stock.post.yaml", "name: stock.post\nmajor: 1\ntitle: Stock\nactions:\n  - {name: post, title: Post}\n")
	write("contracts/stock.post@2.yaml", "name: stock.post\nmajor: 2\ntitle: Stock\nactions:\n  - {name: post, title: Post}\n  - {name: reverse, title: Reverse}\n")
	write("locales/zh-CN.yaml", "Stock: 库存\nPost: 过账\nReverse: 冲销\n")
	b, err := lockPackage(dir)
	if err != nil {
		t.Fatal(err)
	}
	assets := b.descriptor.Blueprint.Assets
	if len(assets) != 2 {
		t.Fatalf("compiled %d assets, expected two contract versions", len(assets))
	}
	for _, name := range []string{"stock.post", "stock.post@2"} {
		ref := platform.AssetRef{App: "build", Kind: protocol.AssetContract, Name: name}
		if !slices.ContainsFunc(assets, func(a platform.ReleaseAsset) bool { return a.Ref == ref }) {
			t.Fatalf("missing %s", ref)
		}
	}
	resources, err := readResources(dir)
	if err != nil {
		t.Fatal(err)
	}
	paths, err := layout(resources)
	if err != nil {
		t.Fatal(err)
	}
	if paths[assets[0].Ref] == paths[assets[1].Ref] {
		t.Fatal("contract major versions export to the same file")
	}
}
