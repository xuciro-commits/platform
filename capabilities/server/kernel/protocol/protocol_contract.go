package protocol

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"platformserver/kernel/values"
	"platformserver/platform"
)

const AssetContract platform.AssetKind = "contract"

var contractName = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[.-][a-z0-9]+)*$`)

type ProtocolContract struct {
	Name        string                   `json:"name"`
	Major       int                      `json:"major"`
	Title       string                   `json:"title"`
	Description string                   `json:"description,omitempty"`
	Actions     []ContractAction         `json:"actions,omitempty"`
	Reads       []string                 `json:"reads,omitempty"`
	Events      []platform.ProtocolEvent `json:"events,omitempty"`
}

type ContractAction struct {
	Name        string           `json:"name"`
	Title       string           `json:"title"`
	Description string           `json:"description,omitempty"`
	Payload     []platform.Field `json:"payload,omitempty"`
}

func (c ProtocolContract) ID() string { return fmt.Sprintf("%s/%d", c.Name, c.Major) }

func (c ProtocolContract) Protocol() platform.Protocol {
	p := platform.Protocol{Name: c.Name, Version: c.Major, Reads: slices.Clone(c.Reads), Events: slices.Clone(c.Events)}
	for _, action := range c.Actions {
		p.Actions = append(p.Actions, platform.Action{Schema: action.Name, Title: action.Title, Description: action.Description, Payload: slices.Clone(action.Payload)})
	}
	return p
}

func (c ProtocolContract) Check() error {
	if len(c.Name) > 120 || !contractName.MatchString(c.Name) || c.Major < 1 || c.Major > 999 || strings.TrimSpace(c.Title) == "" || len(c.Title) > 200 || len(c.Description) > 4000 {
		return fmt.Errorf("A capability contract needs a lowercase dotted name, a major version from 1 to 999 and a title")
	}
	if len(c.Actions) == 0 && len(c.Reads) == 0 || len(c.Actions) > 64 || len(c.Reads) > 64 || len(c.Events) > 64 {
		return fmt.Errorf("A capability contract needs 1–64 actions or reads and at most 64 events")
	}
	seen := map[string]bool{}
	for _, action := range c.Actions {
		if !values.Identifier.MatchString(action.Name) || seen["action:"+action.Name] || strings.TrimSpace(action.Title) == "" || len(action.Title) > 200 || len(action.Description) > 4000 {
			return fmt.Errorf("Capability contract actions need unique lowercase names and titles")
		}
		seen["action:"+action.Name] = true
		if err := platform.CheckInputs(action.Payload); err != nil {
			return err
		}
	}
	for _, read := range c.Reads {
		if !values.Identifier.MatchString(read) || seen["read:"+read] {
			return fmt.Errorf("Capability contract reads need unique lowercase names")
		}
		seen["read:"+read] = true
	}
	for _, event := range c.Events {
		if !values.Identifier.MatchString(event.Name) || seen["event:"+event.Name] || strings.TrimSpace(event.Title) == "" || len(event.Title) > 200 {
			return fmt.Errorf("Capability contract events need unique lowercase names and titles")
		}
		seen["event:"+event.Name] = true
	}
	return nil
}

func CheckContractImplementation(c ProtocolContract, implemented platform.Protocol) error {
	if err := CheckProtocol(c.Protocol(), implemented); err != nil {
		return fmt.Errorf("An installed implementation does not match capability contract %s: %w", c.ID(), err)
	}
	return nil
}
