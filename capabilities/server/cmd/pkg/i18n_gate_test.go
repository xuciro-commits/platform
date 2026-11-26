package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestI18nGateCapabilityPackageAndMissingKeys(t *testing.T) {
	dir := t.TempDir()
	mustWrite := func(rel, content string) {
		t.Helper()
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("package.yaml", `id: voice
version: 0.1.0
title: Voice calls
namespace: voice
description: Outbound voice call capability contract.
roots:
  - app: build
    kind: contract
    name: voice
artifact: voice.json
`)
	mustWrite("contracts/voice.yaml", `name: voice
major: 1
title: Voice calls
description: Place an outbound call.
actions:
  - name: dial
    title: Dial
    description: Dial a phone number.
    payload:
      - {name: number, label: Phone number, type: text, required: true}
`)
	mustWrite("locales/zh-CN.yaml", `Voice calls: 语音通话
Dial: 拨打
Dial a phone number.: 拨打号码。
Place an outbound call.: 发起外呼。
`)

	if _, err := lockPackage(dir); err == nil || !strings.Contains(err.Error(), "Outbound voice call capability contract.") || !strings.Contains(err.Error(), "Phone number") {
		t.Fatalf("expected missing package description and action payload label to fail i18n gate, got: %v", err)
	}

	mustWrite("locales/zh-CN.yaml", `Voice calls: 语音通话
Outbound voice call capability contract.: 外呼语音通话能力契约。
Dial: 拨打
Dial a phone number.: 拨打号码。
Place an outbound call.: 发起外呼。
Phone number: 电话号码
`)
	b, err := lockPackage(dir)
	if err != nil {
		t.Fatalf("capability package with complete locales failed lock: %v", err)
	}
	if b.descriptor.Languages["zh-CN"]["Voice calls"] != "语音通话" {
		t.Fatalf("expected package descriptor languages to include zh-CN, got: %+v", b.descriptor.Languages)
	}
	entry := catalogEntry(b, "https://example.com/voice.json")
	if entry.Locales["zh-CN"].Title != "语音通话" || entry.Locales["zh-CN"].Description != "外呼语音通话能力契约。" {
		t.Fatalf("expected catalogEntry to carry zh-CN locales, got: %+v", entry.Locales)
	}
}

func TestI18nGateRejectsUntranslatedRoleAndChoice(t *testing.T) {
	dir := t.TempDir()
	mustWrite := func(rel, content string) {
		t.Helper()
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("package.yaml", `id: desk
version: 0.1.0
title: Desk
namespace: desk
description: Support tickets.
roots:
  - app: build
    kind: app
    name: desk
artifact: desk.json
`)
	mustWrite("applications/desk.yaml", `name: desk
title: Desk
icon: inbox
pages: [desk.ticket]
`)
	mustWrite("objects/ticket/object.yaml", `name: ticket
namespace: desk
title: Ticket
plural: Tickets
icon: ticket
display: subject
fields:
  - {name: subject, title: Subject, type: text, required: true}
  - {name: severity, title: Severity, type: choice, choices: "minor, urgent"}
access:
  - {role: dispatcher, read: all, create: true, edit: true}
`)
	mustWrite("locales/zh-CN.yaml", `Desk: 服务台
Support tickets.: 支持工单。
Ticket: 工单
Tickets: 工单
Subject: 主题
Severity: 严重程度
`)
	if _, err := lockPackage(dir); err == nil || !strings.Contains(err.Error(), "dispatcher") || !strings.Contains(err.Error(), "minor") {
		t.Fatalf("expected untranslated role and plain choice to fail i18n gate, got: %v", err)
	}

	mustWrite("locales/zh-CN.yaml", `Desk: 服务台
Support tickets.: 支持工单。
Ticket: 工单
Tickets: 工单
Subject: 主题
Severity: 严重程度
Dispatcher: 调度员
Minor: 轻微
Urgent: 紧急
`)
	if _, err := lockPackage(dir); err != nil {
		t.Fatalf("expected upper-first role and choice translations to pass i18n gate, got: %v", err)
	}
}
