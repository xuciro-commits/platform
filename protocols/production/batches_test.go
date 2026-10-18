package production

import (
	"encoding/json"
	"os"
	"testing"

	"platformserver/platform"
)

func TestBatchesProviderDeclaresTheVersionedContract(t *testing.T) {
	raw, err := os.ReadFile("../../deploy/packages/production-provider.json")
	if err != nil {
		t.Fatal(err)
	}
	var pkg struct {
		Blueprint struct {
			Assets []struct {
				Body struct {
					Protocols []platform.ProtocolBinding `json:"protocols"`
				} `json:"body"`
			} `json:"assets"`
		} `json:"blueprint"`
	}
	if err = json.Unmarshal(raw, &pkg); err != nil {
		t.Fatal(err)
	}
	for _, asset := range pkg.Blueprint.Assets {
		for _, binding := range asset.Body.Protocols {
			if binding.Contract == nil {
				t.Fatal("provider omitted its protocol contract")
			}
			if err := platform.CheckProtocol(BatchesProtocol(), *binding.Contract); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatal("provider omitted its versioned protocol")
}
