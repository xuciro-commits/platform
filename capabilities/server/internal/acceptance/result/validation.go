package result

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"google.golang.org/protobuf/proto"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	"slices"
	"strings"
)

const MaxHops = 100

func SubmissionHash(s *pb.Submission, versions ...*platform.SchemaVersion) (string, error) {
	raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(s)
	if err != nil {
		return "", err
	}
	if len(versions) > 0 && versions[0] != nil {
		binding, err := json.Marshal(versions[0])
		if err != nil {
			return "", err
		}
		raw = append(append(raw, 0), binding...)
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

func ValidateSubmissionSchemaVersion(sub *pb.Submission, binding *platform.SchemaVersion) error {
	if binding == nil {
		return nil
	}
	if err := binding.Check(); err != nil {
		return err
	}
	if sub.GetSchema().GetVersion() != binding.Family {
		return fmt.Errorf("submission family differs from its saved schema binding")
	}
	return nil
}

func ValidateBusinessFact(app string, f platform.BusinessRefusalFact) error {
	if !slices.Contains([]string{"block", "flag", "route", "rejected"}, f.Level) || f.Code == "" || len(f.Code) > 64 || len(f.Message) > 1024 || f.Message == "" || f.Member == "" || f.At.IsZero() || !strings.HasPrefix(f.Subject, app+".") || !strings.Contains(f.Subject, "/") || !strings.HasPrefix(f.Action, app+".") || len(f.Subject) > 512 || len(f.Original) > 64<<10 || !json.Valid(f.Original) || len(f.Inputs) > 64 || len(f.Translations) > 1 {
		return fmt.Errorf("invalid business refusal evidence")
	}
	for language, message := range f.Translations {
		if language != "zh-CN" || len(message) > 1024 {
			return fmt.Errorf("invalid business refusal translation")
		}
	}
	for name, raw := range f.Inputs {
		if len(name) > 128 || len(raw) > 64<<10 || !json.Valid(raw) {
			return fmt.Errorf("invalid business refusal input")
		}
	}
	if f.Escalation != nil {
		if err := f.Escalation.Check(); err != nil {
			return err
		}
	}
	if f.Level == "route" && (f.Route == nil || f.Route.Action == "") {
		return fmt.Errorf("business refusal has no declared route")
	}

	return nil
}

// An explicit app reads the historical single-authority witness. New batches
// bind each witness to its own accepted child; DecodeBatch verifies that receipt.
func ValidateTxnIterations(app string, iterations []platform.TxnIteration) error {
	total, steps := 0, map[string]bool{}
	for _, iteration := range iterations {
		owner, key := app, iteration.Step
		if app == "" {
			var change string
			owner, change, _ = strings.Cut(iteration.Decision, "/")
			if owner == "" || change == "" || strings.Contains(change, "/") {
				return fmt.Errorf("transaction iteration has no decision identity")
			}
			key = iteration.Decision + "/" + iteration.Step
		} else if iteration.Decision != "" {
			return fmt.Errorf("legacy transaction iteration carries a decision identity")
		}
		hash, err := hex.DecodeString(iteration.Definition)
		if err != nil || len(hash) != 32 || iteration.Step == "" || iteration.Set == "" || steps[key] || !strings.HasPrefix(iteration.Object, owner+".") {
			return fmt.Errorf("transaction iteration witness is invalid")
		}
		steps[key] = true
		ids := map[string]bool{}
		for _, id := range iteration.IDs {
			if id == "" || ids[id] {
				return fmt.Errorf("transaction iteration identities must be unique")
			}
			ids[id] = true
		}
		total += len(iteration.IDs)
		if total > platform.TxnMaxIterations {
			return fmt.Errorf("transaction has %d iterations; limit is %d", total, platform.TxnMaxIterations)
		}
	}
	return nil
}
