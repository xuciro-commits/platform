# Platform Kernel Core Architecture Bundle

## `contract/spec/README.md`
```markdown
# 内核契约规范 (v1alpha1)

定义内核契约假说 K1–K9 的语义规则、错误代码与一致性测试向量 (ADR-0002)。

## 契约地图

| 假说 | 语义规范 | Protobuf 模式 | 一致性测试向量 | 错误规范 |
|---|---|---|---|---|
| **K1** 身份标识 | [K1-identity.md](K1-identity.md) | `proto/platform/kernel/v1alpha1/identity.proto` | `vectors/k1-identity.json` | `errors.md` |
| **K2/K3** 事实种类与来源出处 | [K2-K3-facts.md](K2-K3-facts.md) | `proto/platform/kernel/v1alpha1/fact.proto` | `vectors/k2-k3-facts.json` | `errors.md` |
| **K4** 变更记录 | [K4-change-record.md](K4-change-record.md) | `proto/platform/kernel/v1alpha1/change.proto` | `vectors/k4-change-record.json` | `errors.md` |
| **K5** 权威归属与同步 | [K5-authority.md](K5-authority.md) | `proto/platform/kernel/v1alpha1/authority.proto` | `vectors/k5-authority.json`, `vectors/k5-migration.json` | `errors.md` |
| **K6** 租户隔离与策略 | [K6-tenancy-policy.md](K6-tenancy-policy.md) | `proto/platform/kernel/v1alpha1/tenancy.proto` | `vectors/k6-receive.json` | `errors.md` |
| **K7** 模式版本演进 | [K7-schema-evolution.md](K7-schema-evolution.md) | `proto/platform/kernel/v1alpha1/schema.proto` | `vectors/k7-schema-evolution.json` | `errors.md` |
| **K8** 连接器 | [K8-connectors.md](K8-connectors.md) | `proto/platform/kernel/v1alpha1/connector.proto` | `vectors/k8-connectors.json` | `errors.md` |
| **K9** 工作所有权 | [K9-work-ownership.md](K9-work-ownership.md) | `proto/platform/kernel/v1alpha1/work.proto` | `vectors/k9-work.json` | `errors.md` |
| **错误规范** | [errors.md](errors.md) | `proto/platform/kernel/v1alpha1/error.proto` | — | — |

每项规则定义必须满足 (MUST) 与严禁违反 (MUST NOT) 的语义，以及违背规则时返回的内核错误代码。Go 参考实现 (`contract/go/`) 验证所有测试向量；Rust (`apps/pms/client/`) 与 TypeScript (`web/packages/kernel/`) 的边缘客户端运行 K5 系列测试向量。

```

## `contract/spec/errors.md`
```markdown
# 错误模型 (契约 v1alpha1)

模式定义：`proto/platform/kernel/v1alpha1/error.proto`。被拒绝的操作返回一个 `Error`。仅 `code` 属于契约：具体实现与测试向量比对错误代码，绝不比对提示信息。错误代码绝不重新编号或复用。

| 错误代码 | 业务含义 |
|---|---|
| `INVALID_ARGUMENT` | 请求格式错误或缺失必填字段。 |
| `NOT_FOUND` | 操作的目标主体不存在。 |
| `CONFLICT` | 操作与既有状态相矛盾（例如针对同一个引用设立第二个重定向）。 |
| `IDEMPOTENCY_CONFLICT` | 幂等键被复用于另一项不同的请求。 |
| `REDIRECT_CYCLE` | 重定向会导致某个引用可达自身，构成循环。 |
| `INVALID_REFERENCE` | 所引用的实体或变更在作用域内不存在。 |
| `UNKNOWN_SCHEMA` | 有效负载的模式或版本未知且无法向上平滑升级。 |
| `POLICY_DENIED` | 授权策略拒绝了该操作主体 (K6)。 |
| `NOT_AUTHORITY` | 接收方不是该数据类别的已声明权威 (K5)。 |

新增错误代码属于次要契约变更；改变既有错误代码的返回时机属于破坏性变更（详见 `docs/Platform.md` 内核契约一节）。

```

## `contract/spec/K1-identity.md`
```markdown
# K1 身份标识 — 语义规范 (契约 v1alpha1)

模式定义：`proto/platform/kernel/v1alpha1/identity.proto`。一致性测试向量：`vectors/k1-identity.json`。错误代码：`errors.md`。关键字 MUST/MUST NOT/SHOULD 遵循 RFC 2119 规范。

## 概念模型

- **实体引用 (entity reference)** 是二元组 (`type`, `id`)。两个引用仅在两部分均完全相同时方才相等：在两个不同类型下的相同 `id` 指向两个不同的实体。
- `id` 是不透明的。具体实现严禁 (MUST NOT) 从中推导业务含义，严禁 (MUST NOT) 将某个 `id` 复用于另一个实体，即使在它被重定向废弃淘汰后亦不可复用。
- 外部标识符（商品目录编码、文件路径、服务端主键）是关于实体的断言，而不是身份本身，属于 K1 范畴之外。
- **重定向 (redirect)** 淘汰其 `from` 引用并指向一个目标（合并 merge）或两个及以上目标（拆分 split）。重定向属于历史事实：绝不修改或删除。

## 语义规则

| 编号 | 规则描述 | 违规返回错误 |
|---|---|---|
| I1 | 存在且未设立重定向的引用解析为其自身。 | — |
| I2 | 解析未知引用必须失败。 | `NOT_FOUND` |
| I3 | 合并重定向必须且仅有一个目标；拆分重定向至少有两个目标；类别不得为 `UNSPECIFIED`。 | `INVALID_ARGUMENT` |
| I4 | 重定向的来源 `from` 引用必须已存在。 | `NOT_FOUND` |
| I5 | 重定向的每个目标引用必须均已存在。 | `INVALID_REFERENCE` |
| I6 | 一个引用最多只能设立一条重定向。 | `CONFLICT` |
| I7 | 重定向绝不能导致任何引用自可达，包括指向自身的重定向。 | `REDIRECT_CYCLE` |
| I8 | 解析过程传递式追踪重定向。若所有路径均终止于同一个终态引用，则解析结果为 `resolved`；否则解析结果为 `ambiguous`（歧义），按已声明目标的深度优先顺序列出互不相同的终态引用。 | — |
| I9 | 被拒绝的操作不改变系统状态。 | — |
| I10 | 实体在其被创建后即宣告存在：由产生它的决策 (K4) 或派生推导 (K2) 生成。创建已存在或已被重定向淘汰的引用必须失败；ID 绝不复用（概念模型）。 | `CONFLICT`, `INVALID_ARGUMENT`（类型或 ID 为空） |

## 补充说明

- `from` 引用与目标引用的类型 `type` 允许不同：一个实体类型可以拆分为全新的实体类型 (K7)。
- 在出现 `ambiguous`（歧义）候选目标时做出取舍选择属于领域业务决策 (K4)，内核绝不做出隐式裁决。
- 记录谁创建了重定向以及为何创建，归属于承载该重定向的 K4 变更记录。

```

## `contract/spec/K4-change-record.md`
```markdown
# K4 变更记录 — 语义规范 (契约 v1alpha1)

模式定义：`proto/platform/kernel/v1alpha1/change.proto`。一致性测试向量：`vectors/k4-change-record.json`。错误代码：`errors.md`。

## 概念模型

- **提交 (submission)** 是一项拟执行的决策提议。接受该提议的权威 (K5) 通过为其分配 `change_id` 与 `recorded_time` 将其转化为一条**变更记录 (change record)**，或返回错误予以拒绝。
- 租户被接受的变更记录构成一个仅追加的**日志 (log)**。记录一旦生成不可篡改；历史事实绝不重写。
- `valid_time` 是决策在实际业务中正式生效的时间；`recorded_time` 是权威获知该决策的时间。它们是不同的客观事实，严禁 (MUST NOT) 相互替代。
- `payload` 是由 `schema` 描述的业务领域数据；内核不对其内部做业务层面的解读。

## 语义规则

| 编号 | 规则描述 | 违规返回错误 |
|---|---|---|
| C1 | `tenant_id`、`principal_id`、`authority`、`idempotency_key`、`target.type`、`target.id` 与 `schema.name` 均为必填字段。 | `INVALID_ARGUMENT` |
| C2 | 接收方必须能够接受并解析该 `schema` (K7 S1)。 | `UNKNOWN_SCHEMA` |
| C3 | 非空的 `causation_id` 必须指向同一租户内已接受的变更记录。 | `INVALID_REFERENCE` |
| C4 | 幂等性作用域限定在 (`tenant_id`, `idempotency_key`)。重新提交完全相同的提交提议，必须返回原先已被接受的记录（相同的 `change_id` 与时间戳）且不追加任何新数据。 | — |
| C5 | 在相同作用域的键下，提交带有任何相异字段的提议均予以拒绝。 | `IDEMPOTENCY_CONFLICT` |
| C6 | `recorded_time` 由权威分配，且在同一租户日志内单调递增不减：其数值为权威物理时钟与上一条记录 `recorded_time` 之间的较大者。 | — |
| C7 | 若缺失 `valid_time` 则自动取 `recorded_time` 的值；若显式提供则按提交原样保留，允许为过去的历史时间。 | — |
| C8 | 更正或撤销操作是一条全新的变更记录，其 `causation_id` 指明所更正的原变更记录。被更正的原记录保持原状。 | — |
| C9 | 被拒绝的提交不追加任何日志分录。 | — |
| C10 | 领域业务规则在 C1–C5 之后且在日志追加之前进行评估：幂等重放 (C4) 直接返回原始记录，绝不重新评估业务规则；领域层拒绝则返回其自身的错误代码且不追加日志。 | 领域专属错误代码 |
| C12 | 目标的**修订版本号 (revision)** 是指命名该目标的历史已被接受变更的总数（首次变更前为 0）；每条记录携带其所产生的最新修订号。带有 `expected_revision` 的提交仅在目标的当前修订号与其严格相等时方可被接受，从而使基于陈旧数据视图做出的决策被拒绝而不是静默覆盖。幂等重放 (C4) 无视此项直接返回原始记录。 | `CONFLICT` |
| C11 | `evidence_fact_ids` 中的每个条目必须指向同一租户内部已记录的事实 (K2)：即该决策所依据的客观观察与断言。 | `INVALID_REFERENCE` |

## 补充说明

- 鉴权授权与完整的请求接收处理顺序见 K6 (`K6-tenancy-policy.md`)。
- 拒绝状态不被记忆：相同的幂等键可以再次提交并在后续成功执行 (C9)。因此发送方仅在未收到应答时（K5 `UNKNOWN`）重试相同的键，绝不在被明确拒绝后重试。
- C12 取代了此前两个垂直切片在各自有效负载中私自携带的前置检查条件（摩擦项 F-20）。领域专属的业务条件依然归属于 C10。
- `correlation_id` 用于将相关变更分组以便于链路追踪；v1alpha1 未对其绑定强制规则。
- 同一权威下的多项变更同生共灭：决策规则一次性返回覆盖它们全部的单次应用。跨多个权威原子性分组变更的设计已被正式拒绝 (ADR-0026)：一项决策仅变更单一权威的数据，对其他权威的诉求在该决策被接受后由其发起请求并等待对方应答。

```

## `contract/spec/K6-tenancy-policy.md`
```markdown
# K6 租户隔离与策略 — 语义规范 (契约 v1alpha1)

模式定义：`proto/platform/kernel/v1alpha1/tenancy.proto`。一致性测试向量：`vectors/k6-receive.json`。错误代码：`errors.md`。

## 概念模型

- **租户 (tenant)** 是面向数据、配置与审计的强隔离边界，而不是具体的组织架构模式。个人空间是具备单一操作主体与设备端权威的退化租户。
- **调用者 (caller)** 是接收方通过任意底层传输协议完成身份认证后的主体凭据；内核仅规定提交提议必须与其严格绑定。
- **策略 (policy)** 由业务领域提供，针对每项全新的提交提议执行一次求值评估，输入参数为主体凭据、业务动作（有效负载的模式名称）及目标实体。组织架构（部门、工厂、岗位角色）属于策略可以查阅的领域数据；内核绝不对其进行解析。
- **接收处理 (receiving)** 是权威向提交提议应用内核规则的确定性固定处理顺序 (T2)。业务领域仅可接入授权策略及其专属业务规则 (K4 C10)，绝无其他侵入点。

## 语义规则

| 编号 | 规则描述 | 违规返回错误 |
|---|---|---|
| T1 | 提交中的 `tenant_id` 与 `principal_id` 必须与调用者的身份凭据严格相等。 | `POLICY_DENIED` |
| T2 | 固定的接收处理顺序：T1；K4 C1, C2；幂等重放检测 (C4, C5)；C3, C11, C12；K5 A3；策略评估 (T3)；领域业务规则 (C10)；日志追加 (C6, C7)。首个违背的规则决定最终返回的错误。幂等重放直接返回原始记录，绝不重新评估 A3、策略或领域业务规则。 | — |
| T3 | 对每项全新的提交提议执行策略评估；策略显式拒绝则驳回该操作。 | `POLICY_DENIED` |
| T4 | 每次读取操作的作用域均严格限制在调用者所在的租户内部；任何操作绝不返回或跨租户引用另一个租户的记录 (K4 C3, C11)。 | — |

## 补充说明

- 幂等重放跳过策略评估 (T2)：在某项决策生效后撤销权限绝不重写历史事实；重放仅原样返回当时已被正式接受的内容。
- 审计系统即为变更日志本身：每项已被接受的决策均记录其操作主体与权威归属 (K3 P4)。

```

## `contract/proto/platform/kernel/v1alpha1/error.proto`
```protobuf
// Kernel error model. Only `code` is contract; `message` is for humans.
syntax = "proto3";

package platform.kernel.v1alpha1;

enum ErrorCode {
  ERROR_CODE_UNSPECIFIED = 0;
  ERROR_CODE_INVALID_ARGUMENT = 1;
  ERROR_CODE_NOT_FOUND = 2;
  ERROR_CODE_CONFLICT = 3;
  ERROR_CODE_IDEMPOTENCY_CONFLICT = 4;
  ERROR_CODE_REDIRECT_CYCLE = 5;
  ERROR_CODE_INVALID_REFERENCE = 6;
  ERROR_CODE_UNKNOWN_SCHEMA = 7;
  ERROR_CODE_POLICY_DENIED = 8;
  ERROR_CODE_NOT_AUTHORITY = 9;
}

message Error {
  ErrorCode code = 1;
  string message = 2;
  map<string, string> details = 3;
}

```

## `contract/proto/platform/kernel/v1alpha1/identity.proto`
```protobuf
// K1 Identity — data contract only. Meaning: contract/spec/K1-identity.md.
syntax = "proto3";

package platform.kernel.v1alpha1;

// Typed, opaque reference to an entity. `id` is never parsed and never reused.
message EntityRef {
  string type = 1;
  string id = 2;
}

enum RedirectKind {
  REDIRECT_KIND_UNSPECIFIED = 0;
  REDIRECT_KIND_MERGE = 1; // exactly one target
  REDIRECT_KIND_SPLIT = 2; // two or more targets
}

// Keeps a retired ID resolvable after a merge or split.
message Redirect {
  EntityRef from = 1;
  repeated EntityRef to = 2;
  RedirectKind kind = 3;
}

message Candidates {
  repeated EntityRef refs = 1;
}

message Resolution {
  oneof outcome {
    EntityRef resolved = 1;
    Candidates ambiguous = 2;
  }
}

```

## `contract/proto/platform/kernel/v1alpha1/change.proto`
```protobuf
// K4 Change record — data contract only. Meaning: contract/spec/K4-change-record.md.
syntax = "proto3";

package platform.kernel.v1alpha1;

import "google/protobuf/timestamp.proto";
import "platform/kernel/v1alpha1/identity.proto";

message SchemaRef {
  string name = 1;
  uint32 version = 2;
}

// A decision as submitted. The accepting authority assigns id and recorded time.
message Submission {
  string tenant_id = 1;
  string principal_id = 2;
  string authority = 3;
  EntityRef target = 4;
  SchemaRef schema = 5;
  google.protobuf.Timestamp valid_time = 6;
  string causation_id = 7;
  string correlation_id = 8;
  string idempotency_key = 9;
  bytes payload = 10;
  // Recorded facts (K2) this decision is based on.
  repeated string evidence_fact_ids = 11;
  // Precondition: the target's revision the submitter saw (C12). Absent: no check.
  optional uint32 expected_revision = 12;
}

// An accepted decision. Immutable once recorded.
message ChangeRecord {
  string change_id = 1;
  Submission submission = 2;
  google.protobuf.Timestamp valid_time = 3;
  google.protobuf.Timestamp recorded_time = 4;
  // The target's revision after this change: accepted changes naming it so far (C12).
  uint32 revision = 5;
}

```

## `contract/go/kernel/errors.go`
```go
package kernel

import pb "platformkernel/gen/platform/kernel/v1alpha1"

// Error carries a contract error code (contract/spec/errors.md) and, for
// people, why: the message is never contract, and vectors compare codes only.
type Error struct {
	Code    pb.ErrorCode
	Message string
}

func (e *Error) Error() string { return e.Code.String() }

func errorf(code pb.ErrorCode) *Error { return &Error{Code: code} }

```

## `contract/go/kernel/identity.go`
```go
// Package kernel is the Go reference implementation of the kernel contract.
// The contract itself is contract/proto, contract/spec and contract/vectors.
package kernel

import (
	"slices"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
)

// Ref is a comparable entity reference.
type Ref struct{ Type, ID string }

func RefOf(r *pb.EntityRef) Ref { return Ref{r.GetType(), r.GetId()} }

// Identity implements K1 (contract/spec/K1-identity.md).
type Identity struct {
	entities  map[Ref]bool
	redirects map[Ref][]Ref
}

func NewIdentity(entities []*pb.EntityRef) *Identity {
	id := &Identity{entities: map[Ref]bool{}, redirects: map[Ref][]Ref{}}
	for _, e := range entities {
		id.entities[RefOf(e)] = true
	}
	return id
}

// Create brings a new entity into existence (I10); IDs are never reused.
func (id *Identity) Create(r *pb.EntityRef) *Error {
	ref := RefOf(r)
	if ref.Type == "" || ref.ID == "" {
		return errorf(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	}
	if id.entities[ref] {
		return errorf(pb.ErrorCode_ERROR_CODE_CONFLICT)
	}
	id.entities[ref] = true
	return nil
}

func (id *Identity) AddRedirect(r *pb.Redirect) *Error {
	from, to := RefOf(r.GetFrom()), make([]Ref, 0, len(r.GetTo()))
	for _, t := range r.GetTo() {
		to = append(to, RefOf(t))
	}
	switch {
	case r.GetKind() == pb.RedirectKind_REDIRECT_KIND_MERGE && len(to) != 1,
		r.GetKind() == pb.RedirectKind_REDIRECT_KIND_SPLIT && len(to) < 2,
		r.GetKind() == pb.RedirectKind_REDIRECT_KIND_UNSPECIFIED:
		return errorf(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT) // I3
	case !id.entities[from]:
		return errorf(pb.ErrorCode_ERROR_CODE_NOT_FOUND) // I4
	case slices.ContainsFunc(to, func(t Ref) bool { return !id.entities[t] }):
		return errorf(pb.ErrorCode_ERROR_CODE_INVALID_REFERENCE) // I5
	case id.redirects[from] != nil:
		return errorf(pb.ErrorCode_ERROR_CODE_CONFLICT) // I6
	case slices.ContainsFunc(to, func(t Ref) bool { return id.reaches(from, t) }):
		return errorf(pb.ErrorCode_ERROR_CODE_REDIRECT_CYCLE) // I7
	}
	id.redirects[from] = to
	return nil
}

// Resolve returns one terminal reference, or several when a split leaves a choice (I8).
func (id *Identity) Resolve(r *pb.EntityRef) ([]Ref, *Error) {
	ref := RefOf(r)
	if !id.entities[ref] {
		return nil, errorf(pb.ErrorCode_ERROR_CODE_NOT_FOUND) // I2
	}
	var terminals []Ref
	var walk func(Ref)
	walk = func(current Ref) {
		if targets := id.redirects[current]; targets != nil {
			for _, t := range targets {
				walk(t)
			}
		} else if !slices.Contains(terminals, current) {
			terminals = append(terminals, current)
		}
	}
	walk(ref)
	return terminals, nil
}

func (id *Identity) reaches(goal, start Ref) bool {
	if start == goal {
		return true
	}
	return slices.ContainsFunc(id.redirects[start], func(t Ref) bool { return id.reaches(goal, t) })
}

```

## `contract/go/kernel/change.go`
```go
package kernel

import (
	"fmt"
	"slices"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
)

// ChangeLog implements K4 (contract/spec/K4-change-record.md) for one authority.
type ChangeLog struct {
	schemas *SchemaRegistry
	logs    map[string][]*pb.ChangeRecord
	byKey   map[string]map[string]*pb.ChangeRecord
	revs    map[[3]string]uint32 // (tenant, target type, target id) → revision (C12)
	next    int
	// Facts reports whether a fact is recorded in a tenant (K2); nil knows none (C11).
	Facts func(tenant, factID string) bool
}

func NewChangeLog(schemas *SchemaRegistry) *ChangeLog {
	return &ChangeLog{schemas: schemas, logs: map[string][]*pb.ChangeRecord{},
		byKey: map[string]map[string]*pb.ChangeRecord{}, revs: map[[3]string]uint32{}}
}

func (l *ChangeLog) Records(tenant string) []*pb.ChangeRecord { return slices.Clone(l.logs[tenant]) }

// Fork gives one decision a private change-log view. Accepted records are
// immutable; the maps and slices that SubmitChecked extends are independent.
// The host can discard this view when a durable append fails (ADR-0038 19a).
func (l *ChangeLog) Fork() *ChangeLog {
	copy := &ChangeLog{schemas: l.schemas, logs: make(map[string][]*pb.ChangeRecord, len(l.logs)),
		byKey: make(map[string]map[string]*pb.ChangeRecord, len(l.byKey)), revs: make(map[[3]string]uint32, len(l.revs)), next: l.next, Facts: l.Facts}
	for tenant, records := range l.logs {
		copy.logs[tenant] = slices.Clone(records)
	}
	for tenant, keys := range l.byKey {
		copy.byKey[tenant] = make(map[string]*pb.ChangeRecord, len(keys))
		for key, record := range keys {
			copy.byKey[tenant][key] = record
		}
	}
	for target, revision := range l.revs {
		copy.revs[target] = revision
	}
	return copy
}

// ApplyAccepted advances a log from the saved decision, without running its
// policy or business rules. It rejects a missing predecessor or conflicting
// receipt; reapplying the same saved record is a no-op. The host calls this
// only after the result's durable append or while recovering it (ADR-0038).
func (l *ChangeLog) ApplyAccepted(saved *pb.ChangeRecord) (bool, error) {
	if saved == nil || saved.GetSubmission() == nil || saved.GetRecordedTime() == nil || saved.GetValidTime() == nil {
		return false, fmt.Errorf("accepted change is incomplete")
	}
	s := saved.GetSubmission()
	if s.GetTenantId() == "" || s.GetIdempotencyKey() == "" || s.GetTarget().GetType() == "" || s.GetTarget().GetId() == "" {
		return false, fmt.Errorf("accepted change has no scoped identity")
	}
	if prior := l.byKey[s.GetTenantId()][s.GetIdempotencyKey()]; prior != nil {
		if proto.Equal(prior, saved) {
			return false, nil
		}
		return false, fmt.Errorf("accepted change conflicts with an earlier receipt")
	}
	if saved.GetChangeId() != fmt.Sprintf("chg-%d", l.next+1) {
		return false, fmt.Errorf("accepted change %s is not next after %d", saved.GetChangeId(), l.next)
	}
	target := [3]string{s.GetTenantId(), s.GetTarget().GetType(), s.GetTarget().GetId()}
	if saved.GetRevision() != l.revs[target]+1 {
		return false, fmt.Errorf("accepted change %s has revision %d after %d", saved.GetChangeId(), saved.GetRevision(), l.revs[target])
	}
	log := l.logs[s.GetTenantId()]
	if len(log) > 0 && saved.GetRecordedTime().AsTime().Before(log[len(log)-1].GetRecordedTime().AsTime()) {
		return false, fmt.Errorf("accepted change %s moves recorded time backwards", saved.GetChangeId())
	}
	record := proto.Clone(saved).(*pb.ChangeRecord)
	l.next++
	l.revs[target] = record.GetRevision()
	l.logs[s.GetTenantId()] = append(log, record)
	if l.byKey[s.GetTenantId()] == nil {
		l.byKey[s.GetTenantId()] = map[string]*pb.ChangeRecord{}
	}
	l.byKey[s.GetTenantId()][s.GetIdempotencyKey()] = record
	return true, nil
}

func (l *ChangeLog) Submit(s *pb.Submission, now time.Time) (*pb.ChangeRecord, *Error) {
	return l.SubmitChecked(s, now, nil)
}

// SubmitChecked runs check after replay detection and before the append (C10);
// a replay returns the original record without running it.
func (l *ChangeLog) SubmitChecked(s *pb.Submission, now time.Time, check func() *Error) (*pb.ChangeRecord, *Error) {
	required := []string{s.GetTenantId(), s.GetPrincipalId(), s.GetAuthority(), s.GetIdempotencyKey(),
		s.GetTarget().GetType(), s.GetTarget().GetId(), s.GetSchema().GetName()}
	if slices.Contains(required, "") {
		return nil, errorf(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT) // C1
	}
	if !l.schemas.Accepts(s.GetSchema()) {
		return nil, errorf(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA) // C2
	}
	if existing := l.byKey[s.GetTenantId()][s.GetIdempotencyKey()]; existing != nil {
		if !proto.Equal(existing.GetSubmission(), s) {
			return nil, errorf(pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT) // C5
		}
		return existing, nil // C4
	}
	log := l.logs[s.GetTenantId()]
	if c := s.GetCausationId(); c != "" && !slices.ContainsFunc(log, func(r *pb.ChangeRecord) bool { return r.GetChangeId() == c }) {
		return nil, errorf(pb.ErrorCode_ERROR_CODE_INVALID_REFERENCE) // C3
	}
	for _, fact := range s.GetEvidenceFactIds() {
		if l.Facts == nil || !l.Facts(s.GetTenantId(), fact) {
			return nil, errorf(pb.ErrorCode_ERROR_CODE_INVALID_REFERENCE) // C11
		}
	}
	target := [3]string{s.GetTenantId(), s.GetTarget().GetType(), s.GetTarget().GetId()}
	if s.ExpectedRevision != nil && s.GetExpectedRevision() != l.revs[target] {
		return nil, errorf(pb.ErrorCode_ERROR_CODE_CONFLICT) // C12
	}
	if check != nil {
		if err := check(); err != nil {
			return nil, err // C10
		}
	}
	recorded := now
	if n := len(log); n > 0 && log[n-1].GetRecordedTime().AsTime().After(now) {
		recorded = log[n-1].GetRecordedTime().AsTime() // C6
	}
	valid := s.GetValidTime()
	if valid == nil {
		valid = timestamppb.New(recorded) // C7
	}
	l.next++
	l.revs[target]++
	record := &pb.ChangeRecord{ChangeId: fmt.Sprintf("chg-%d", l.next), Submission: s,
		ValidTime: valid, RecordedTime: timestamppb.New(recorded), Revision: l.revs[target]}
	l.logs[s.GetTenantId()] = append(log, record)
	if l.byKey[s.GetTenantId()] == nil {
		l.byKey[s.GetTenantId()] = map[string]*pb.ChangeRecord{}
	}
	l.byKey[s.GetTenantId()][s.GetIdempotencyKey()] = record
	return record, nil
}

func schemaKey(s *pb.SchemaRef) string { return fmt.Sprintf("%s@%d", s.GetName(), s.GetVersion()) }

// Adopt takes over a previous authority's accepted records unchanged (K5 A10):
// into an empty tenant log, in recorded order, with unique change IDs and keys.
func (l *ChangeLog) Adopt(records []*pb.ChangeRecord) *Error {
	conflict := errorf(pb.ErrorCode_ERROR_CODE_CONFLICT)
	if len(records) == 0 {
		return nil
	}
	tenant := records[0].GetSubmission().GetTenantId()
	if len(l.logs[tenant]) > 0 {
		return conflict
	}
	ids, keys := map[string]bool{}, map[string]bool{}
	for i, r := range records {
		s := r.GetSubmission()
		if s.GetTenantId() != tenant || ids[r.GetChangeId()] || keys[s.GetIdempotencyKey()] ||
			i > 0 && r.GetRecordedTime().AsTime().Before(records[i-1].GetRecordedTime().AsTime()) {
			return conflict
		}
		ids[r.GetChangeId()], keys[s.GetIdempotencyKey()] = true, true
	}
	l.logs[tenant] = append([]*pb.ChangeRecord(nil), records...)
	l.byKey[tenant] = map[string]*pb.ChangeRecord{}
	for _, r := range records {
		l.byKey[tenant][r.GetSubmission().GetIdempotencyKey()] = r
		l.revs[[3]string{tenant, r.GetSubmission().GetTarget().GetType(), r.GetSubmission().GetTarget().GetId()}]++
	}
	return nil
}

```

## `contract/go/kernel/receiver.go`
```go
package kernel

import (
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
)

// Caller is the principal a receiver authenticated (K6).
type Caller struct{ Tenant, Principal string }

// Policy decides whether a caller may submit s; supplied by the domain (K6 T3).
type Policy func(caller Caller, s *pb.Submission) bool

// Receiver applies the kernel's receiving order (K6 T2) for one authority.
type Receiver struct {
	Changes     *ChangeLog
	Authorities *Authorities
	Policy      Policy
}

// Receive accepts or rejects a submission; domain runs the domain's rules (K4 C10).
func (r *Receiver) Receive(c Caller, s *pb.Submission, now time.Time, domain func() *Error) (*pb.ChangeRecord, *Error) {
	if s.GetTenantId() != c.Tenant || s.GetPrincipalId() != c.Principal {
		return nil, errorf(pb.ErrorCode_ERROR_CODE_POLICY_DENIED) // T1
	}
	return r.Changes.SubmitChecked(s, now, func() *Error {
		if err := r.Authorities.Authorize(s); err != nil {
			return err // A3
		}
		if r.Policy == nil || !r.Policy(c, s) {
			return errorf(pb.ErrorCode_ERROR_CODE_POLICY_DENIED) // T3
		}
		if domain != nil {
			return domain()
		}
		return nil
	})
}

```

## `capabilities/server/platform/ledger.go`
```go
package platform

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
)

// Ledger is one business package's decisions in one tenant: the kernel's change
// log and authority for the package's data classes, received in the kernel's
// order with the package's action catalog as the role check (ADR-0008). The
// package supplies attribute conditions and its rules; it never wires the kernel.
type Ledger struct {
	mu           sync.Mutex
	tenant       string
	authority    string
	declarations []*pb.AuthorityDeclaration
	changes      *kernel.ChangeLog
	authorities  *kernel.Authorities
	// schemas is the change log's registry, kept so the package can teach it a
	// schema that did not exist when it started (K7 S7, ADR-0034).
	schemas *kernel.SchemaRegistry
	Catalog *Catalog
}

// NewLedger declares authority as the tenant server for classes and accepts the
// catalog's actions (version 1 of each schema).
func NewLedger(tenant, authority string, catalog *Catalog, classes ...string) *Ledger {
	var schemas []*pb.SchemaRef
	for _, a := range catalog.actions {
		schemas = append(schemas, &pb.SchemaRef{Name: a.Schema, Version: 1})
	}
	registry := kernel.NewSchemaRegistry(schemas, nil)
	l := &Ledger{tenant: tenant, authority: authority, changes: kernel.NewChangeLog(registry), schemas: registry,
		authorities: kernel.NewAuthorities(authority), Catalog: catalog}
	for _, class := range classes {
		d := &pb.AuthorityDeclaration{TenantId: tenant, DataClass: class, Kind: pb.AuthorityKind_AUTHORITY_KIND_TENANT_SERVER, AuthorityId: authority, Epoch: 1}
		l.authorities.Declare(d)
		l.declarations = append(l.declarations, d)
	}
	return l
}

// Extend declares data classes and actions after composition: what an object a
// tenant defined and published needs of its app's ledger (ADR-0034 D2). The
// kernel takes the new schemas and the authority over the new classes.
// It is called from inside a decision of this ledger — the transition that
// publishes the definition — so the ledger's own lock is already held and is
// not taken again; the catalog it extends carries its own lock for the readers
// outside.
func (l *Ledger) Extend(classes []string, actions []Action) error {
	var schemas []*pb.SchemaRef
	for _, a := range actions {
		if a.Schema == "" || a.Target == "" || a.Title == "" || len(a.Roles) == 0 && !a.Automation {
			return fmt.Errorf("action %q lacks a schema, target, title or roles", a.Schema)
		}
		schemas = append(schemas, &pb.SchemaRef{Name: a.Schema, Version: 1})
	}
	if err := l.schemas.Learn(schemas...); err != nil { // K7 S7
		return err
	}
	l.Catalog.Add(actions...)
	for _, class := range classes {
		if slices.ContainsFunc(l.declarations, func(d *pb.AuthorityDeclaration) bool { return d.GetDataClass() == class }) {
			continue
		}
		d := &pb.AuthorityDeclaration{TenantId: l.tenant, DataClass: class, Kind: pb.AuthorityKind_AUTHORITY_KIND_TENANT_SERVER, AuthorityId: l.authority, Epoch: 1}
		l.authorities.Declare(d)
		l.declarations = append(l.declarations, d)
	}
	return nil
}

// Declarations are the package's authority declarations, for edges (K5 A9).
func (l *Ledger) Declarations() []*pb.AuthorityDeclaration { return l.declarations }

// ApplyAcceptedChange advances the kernel log from a durable result. It does
// not run current catalog policy, rules or a generated action (ADR-0038 19a).
// The boolean says whether this result was new to the log, so the host can
// avoid applying its record images twice after a retry or partial recovery.
func (l *Ledger) ApplyAcceptedChange(record *pb.ChangeRecord) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.changes.ApplyAccepted(record)
}

// ForkAcceptedChanges validates a complete host batch without advancing the
// authoritative log. The tenant commit lock still owns promotion.
func (l *Ledger) ForkAcceptedChanges() *kernel.ChangeLog {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.changes.Fork()
}

// RecordsFor returns a defensive copy of tenant's accepted records,
// safe to call concurrently with Receive.
func (l *Ledger) RecordsFor(tenant string) []*pb.ChangeRecord {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.changes.Records(tenant)
}

// SetFacts supplies the change log's tenant fact-check callback (K2, K4 C11).
func (l *Ledger) SetFacts(facts func(tenant, factID string) bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.changes.Facts = facts
}

// AcceptedFor returns a saved receipt for a key without running application
// decision code. The host uses this before staging a generated action.
func (l *Ledger) AcceptedFor(tenant, key string) *pb.ChangeRecord {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, record := range l.changes.Records(tenant) {
		if record.GetSubmission().GetIdempotencyKey() == key {
			return proto.Clone(record).(*pb.ChangeRecord)
		}
	}
	return nil
}

// Receive accepts s from c or refuses it: an action outside the enabled catalog
// is UNKNOWN_SCHEMA; c's role must be granted and allowed (attribute conditions,
// may be nil) must hold, except in a replay (ADR-0008); rules (may be nil)
// returns how to apply the decision, which runs with the new record only when
// it is accepted (an idempotent replay applies nothing); a new decision is then
// published to the tenant's subscribers (ADR-0010).
func (l *Ledger) Receive(c Caller, s *pb.Submission, now time.Time,
	allowed func() bool, rules func() (func(*pb.ChangeRecord), *kernel.Error)) (*pb.ChangeRecord, *kernel.Error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !c.Replaying && !l.Catalog.Enabled(s.GetSchema().GetName()) {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
	}
	rules = l.checked(c, s, rules)
	probing := c.rt != nil && c.rt.Probing()
	refused := false // the kernel's policy step said no
	changes := l.changes
	// ADR-0038 19a: a host-owned decision view may use a private change log.
	// Ordinary callers and replay retain the existing authoritative path.
	if draft, ok := c.rt.(interface {
		DraftChanges(*Ledger, func() *kernel.ChangeLog) *kernel.ChangeLog
	}); ok {
		changes = draft.DraftChanges(l, l.changes.Fork)
	}
	if probing {
		// Probes validate kernel identity, key conflicts and expected revisions
		// too. Only their private log advances; their apply callback never runs.
		changes = changes.Fork()
	}
	receiver := kernel.Receiver{Changes: changes, Authorities: l.authorities,
		Policy: func(kernel.Caller, *pb.Submission) bool {
			ok := c.Replaying && !probing || c.Automation || l.Catalog.Permits(c.Role(), s.GetSchema().GetName()) && (allowed == nil || allowed())
			refused = !ok
			return ok
		}}
	var apply func(*pb.ChangeRecord)
	record, err := receiver.Receive(kernel.Caller{Tenant: l.tenant, Principal: c.ID}, s, now, func() *kernel.Error {
		if rules == nil {
			return nil
		}
		var err *kernel.Error
		apply, err = rules()
		return err
	})
	if err != nil && err.Code == pb.ErrorCode_ERROR_CODE_POLICY_DENIED && err.Message == "" && refused {
		err = l.denied(c, s) // the kernel's policy step refused: say whose role does not reach
	}
	if probing {
		return nil, err
	}
	if err == nil && apply != nil {
		apply(record)
		if c.rt != nil {
			c.rt.Publish(c, record)
		}
	}
	return record, err
}

// denied is a refusal of the catalog's role check, with why (F-23): the
// member's role in the app, or that they hold none, does not include the action.
func (l *Ledger) denied(c Caller, s *pb.Submission) *kernel.Error {
	action := s.GetSchema().GetName()
	if declared, ok := l.Catalog.Action(action); ok && declared.Title != "" {
		action = declared.Title
	}
	if c.Role() == "" {
		return Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "{member} holds no role in {app}, so may not {action}", c.ID, c.App, action)
	}
	if l.Catalog.Permits(c.Role(), s.GetSchema().GetName()) {
		return Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "{member} may not {action} on this record", c.ID, action)
	}
	return Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The role {role} in {app} may not {action}", c.Role(), c.App, action)
}

// checked puts before rules the check of the payload's declared choices and
// references (ADR-0028 D5): after the policy, as the kernel's order has it
// (K6 T2); a replay does not check again.
func (l *Ledger) checked(c Caller, s *pb.Submission, rules func() (func(*pb.ChangeRecord), *kernel.Error)) func() (func(*pb.ChangeRecord), *kernel.Error) {
	declared, _ := l.Catalog.Action(s.GetSchema().GetName())
	return func() (func(*pb.ChangeRecord), *kernel.Error) {
		if !c.Replaying {
			var payload map[string]any
			json.Unmarshal(s.GetPayload(), &payload)
			for _, f := range declared.Payload {
				v, ok := payload[f.Name].(string)
				if !ok || v == "" {
					continue
				}
				if len(f.Choices) > 0 && !slices.Contains(f.Choices, v) || f.Ref != "" && c.rt != nil && !c.rt.Readable(c, f.Ref+"/"+v) {
					return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
				}
			}
		}
		if rules == nil {
			return nil, nil
		}
		return rules()
	}
}

// Generated decides the actions entity declarations generate: standard
// create, edit and archive (ADR-0016 D5) and lifecycle transitions (ADR-0017
// D1); ok is false for any other schema. allowed (may be nil) adds the app's
// conditions to the catalog's role check.
func (l *Ledger) Generated(c Caller, s *pb.Submission, now time.Time, allowed func() bool, entities ...Entity) (*pb.ChangeRecord, *kernel.Error, bool) {
	schema := s.GetSchema().GetName()
	for _, e := range entities {
		verb, found := strings.CutPrefix(schema, e.Type+".")
		if !found {
			continue
		}
		if verb == "create" && e.Standard.Create || verb == "edit" && e.Standard.Edit || verb == "archive" && e.Standard.Archive {
			record, err := l.Receive(c, s, now, allowed, func() (func(*pb.ChangeRecord), *kernel.Error) {
				return standard(c, e, verb, s)
			})
			return record, err, true
		}
		if e.Lifecycle == nil {
			continue
		}
		if i := slices.IndexFunc(e.Lifecycle.Transitions, func(t Transition) bool { return t.Name == verb }); i >= 0 {
			record, err := l.Receive(c, s, now, allowed, func() (func(*pb.ChangeRecord), *kernel.Error) {
				return transition(c, e, e.Lifecycle.Transitions[i], s, now)
			})
			return record, err, true
		}
		for _, suffix := range []string{ApprovalHeld, ApprovalRejected, ApprovalReturned} {
			name, found := strings.CutSuffix(verb, suffix)
			i := slices.IndexFunc(e.Lifecycle.Transitions, func(t Transition) bool { return t.Name == name && t.Approval != nil && t.Approval.Pending != "" })
			if !found || i < 0 {
				continue
			}
			record, err := l.Receive(c, s, now, allowed, func() (func(*pb.ChangeRecord), *kernel.Error) {
				return transition(c, e, approvalMove(e.Lifecycle.Transitions[i], suffix), s, now)
			})
			return record, err, true
		}
	}
	return nil, nil, false
}

// approvalMove is the move of a record whose transition t waits for approval
// (F-38): into its pending state, or out of it on a rejection or a return.
func approvalMove(t Transition, suffix string) Transition {
	back := t.From[0]
	switch suffix {
	case ApprovalHeld:
		return Transition{Name: t.Name + suffix, From: t.From, To: []string{t.Approval.Pending}}
	case ApprovalRejected:
		if t.Approval.Rejected != "" {
			back = t.Approval.Rejected
		}
	}
	return Transition{Name: t.Name + suffix, From: []string{t.Approval.Pending}, To: []string{back}}
}

// transition moves a record along its lifecycle, through the transition's Do.
// A transition that waits in a pending state also leaves it, once approved.
func transition(c Caller, e Entity, t Transition, s *pb.Submission, now time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	if c.rt == nil {
		return nil, notFound()
	}
	typ := reflect.TypeOf(e.Model)
	existing, known := c.rt.Get(c, typ, s.GetTarget().GetId())
	if !known || !inScope(c, e.Type, s.GetTarget().GetId()) {
		return nil, notFound()
	}
	info, _ := Describe("", e, func(reflect.Type) string { return "?" })
	f, _ := info.Field(e.Lifecycle.Field)
	v := reflect.New(typ)
	v.Elem().Set(reflect.ValueOf(existing))
	status := v.Elem().FieldByIndex(f.Index)
	from := status.String()
	pending := t.Approval != nil && t.Approval.Pending != "" && from == t.Approval.Pending && !c.Automation && !c.rt.Probing() // run by its approval, not asked again
	id := s.GetTarget().GetId()
	if v.Elem().Field(0).Interface().(Record).Archived {
		return nil, Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{record} is archived", id)
	}
	if !slices.Contains(t.From, from) && !pending { // not in a state the transition leaves
		return nil, Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{record} is {state}; {transition} takes it only from {states}", id, from, t.Name, strings.Join(t.From, ", "))
	}
	if t.Do != nil {
		if err := t.Do(c, v.Interface(), s.GetPayload(), now); err != nil {
			return nil, err
		}
	}
	if to := status.String(); to == from && !slices.Contains(t.To, from) {
		status.SetString(t.To[0])
	} else if !slices.Contains(t.To, to) {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT} // Do chose a status the transition does not reach
	}
	value := v.Elem().Interface()
	if err := c.rt.Check(c, value); err != nil {
		return nil, err
	}
	return func(r *pb.ChangeRecord) {
		c.rt.Put(c, r, value)
		if t.After != nil {
			after := reflect.New(typ)
			after.Elem().Set(reflect.ValueOf(value))
			t.After(c, r, after.Interface(), now)
		}
	}, nil
}

func standard(c Caller, e Entity, verb string, s *pb.Submission) (func(*pb.ChangeRecord), *kernel.Error) {
	invalid := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	if c.rt == nil {
		return nil, notFound()
	}
	t := reflect.TypeOf(e.Model)
	id := s.GetTarget().GetId()
	existing, known := c.rt.Get(c, t, id)
	if known && verb != "create" && !inScope(c, e.Type, id) {
		return nil, notFound()
	}
	v := reflect.New(t)
	switch {
	case verb == "create" && known:
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	case verb != "create" && !known:
		return nil, notFound()
	case verb != "create":
		v.Elem().Set(reflect.ValueOf(existing))
	}
	if verb == "archive" {
		v.Elem().Field(0).Addr().Interface().(*Record).Archived = true
	} else {
		var given map[string]json.RawMessage
		if json.Unmarshal(s.GetPayload(), &given) != nil {
			return nil, invalid
		}
		info, _ := Describe("", e, func(reflect.Type) string { return "?" })
		for name := range given {
			if f, ok := info.Field(name); !ok || f.ReadOnly || !c.Replaying && !c.Automation && !f.Writes(c.Role()) {
				return nil, invalid // unknown or read-only fields are never set by a generated action
			}
		}
		// A provided field is a replacement, including a slice/map/struct.
		// Decoding a patch into the old struct reuses slice elements and maps,
		// retaining nested values the caller explicitly removed.
		base, err := json.Marshal(v.Interface())
		var merged map[string]json.RawMessage
		if err != nil || json.Unmarshal(base, &merged) != nil {
			return nil, invalid
		}
		for name, value := range given {
			merged[name] = value
		}
		raw, err := json.Marshal(merged)
		v = reflect.New(t)
		if err != nil || json.Unmarshal(raw, v.Interface()) != nil {
			return nil, invalid
		}
		v.Elem().Field(0).Addr().Interface().(*Record).ID = id
	}
	value := v.Elem().Interface()
	if err := c.rt.Check(c, value); err != nil {
		return nil, err
	}
	return func(r *pb.ChangeRecord) { c.rt.Put(c, r, value) }, nil
}

// inScope says whether the caller may act on a record: only one they may read
// (ADR-0037 18b). A generated action on a record outside the member's scope is
// refused as if it were not there, as a read is. Replay, the app's own
// automation and an approval taking a held step act for the app, not a reader.
func inScope(c Caller, typ, id string) bool {
	return c.Replaying || c.Automation || c.rt.Probing() || c.rt.Readable(c, typ+"/"+id)
}

```

## `contract/lean/KernelProofs/Idempotency.lean`
```lean
/- K4 C4/C5/C9/C10, after C1/C2 validation. / C1/C2 校验后的 K4 幂等分支。
   `body` abstracts every remaining immutable submission field, not just payload.
   body 抽象其余所有不可变提交字段，而非仅 payload。 See README.md for assumptions. -/
namespace KernelProofs

structure Submission where
  tenant : Nat
  key : Nat
  body : List Nat
  deriving DecidableEq

structure Receipt where
  submission : Submission
  identity : Nat
  recordedTime : Nat
  deriving DecidableEq

structure State where
  receipts : List Receipt
  next : Nat
  deriving DecidableEq

def lookup (tenant key : Nat) : List Receipt → Option Receipt
  | [] => none
  | r :: rs => if r.submission.tenant = tenant ∧ r.submission.key = key then
      some r else lookup tenant key rs

inductive Outcome where
  | accepted (receipt : Receipt)
  | replayed (receipt : Receipt)
  | conflict
  | refused
  deriving DecidableEq

structure Result where
  state : State
  outcome : Outcome
  checked : Bool
  deriving DecidableEq

-- The check result is abstract; `checked` records whether the branch invokes it.
-- 检查结果为抽象输入；checked 明确记录该分支是否调用业务检查。
def submit (st : State) (s : Submission) (now : Nat) (allowed : Bool) : Result :=
  match lookup s.tenant s.key st.receipts with
  | some prior => if prior.submission = s then
      ⟨st, .replayed prior, false⟩ else ⟨st, .conflict, false⟩
  | none => if allowed then
      let r : Receipt := ⟨s, st.next + 1, now⟩
      ⟨⟨r :: st.receipts, st.next + 1⟩, .accepted r, true⟩
    else ⟨st, .refused, true⟩

theorem replay_original (st : State) (s : Submission) (r : Receipt) (now : Nat)
    (allowed : Bool) (found : lookup s.tenant s.key st.receipts = some r)
    (same : r.submission = s) :
    submit st s now allowed = ⟨st, .replayed r, false⟩ := by
  simp [submit, found, same]

theorem conflict_unchanged (st : State) (s : Submission) (r : Receipt) (now : Nat)
    (allowed : Bool) (found : lookup s.tenant s.key st.receipts = some r)
    (different : r.submission ≠ s) :
    submit st s now allowed = ⟨st, .conflict, false⟩ := by
  simp [submit, found, different]

theorem refusal_key_free (st : State) (s : Submission) (now : Nat)
    (unused : lookup s.tenant s.key st.receipts = none) :
    submit st s now false = ⟨st, .refused, true⟩ ∧
    lookup s.tenant s.key (submit st s now false).state.receipts = none := by
  simp [submit, unused]

theorem accepted_once (st : State) (s : Submission) (now : Nat)
    (unused : lookup s.tenant s.key st.receipts = none) :
    (submit st s now true).state.receipts.length = st.receipts.length + 1 := by
  simp [submit, unused]

theorem accept_then_replay (st : State) (s : Submission) (now later : Nat)
    (allowed : Bool) (unused : lookup s.tenant s.key st.receipts = none) :
    let accepted := submit st s now true
    submit accepted.state s later allowed =
      ⟨accepted.state, .replayed ⟨s, st.next + 1, now⟩, false⟩ := by
  simp [submit, unused, lookup]

def replayMany (st : State) (s : Submission) (now : Nat) (allowed : Bool) : Nat → State
  | 0 => st
  | n + 1 => replayMany (submit st s now allowed).state s now allowed n

theorem repeat_replay_unchanged (st : State) (s : Submission) (r : Receipt)
    (now : Nat) (allowed : Bool) (n : Nat)
    (found : lookup s.tenant s.key st.receipts = some r) (same : r.submission = s) :
    replayMany st s now allowed n = st := by
  induction n with
  | zero => rfl
  | succ n ih =>
    simp only [replayMany, replay_original st s r now allowed found same]
    exact ih

theorem repeat_replay_length (st : State) (s : Submission) (r : Receipt)
    (now : Nat) (allowed : Bool) (n : Nat)
    (found : lookup s.tenant s.key st.receipts = some r) (same : r.submission = s) :
    (replayMany st s now allowed n).receipts.length = st.receipts.length := by
  rw [repeat_replay_unchanged st s r now allowed n found same]

theorem other_tenant_unchanged (st : State) (s : Submission) (now tenant key : Nat)
    (unused : lookup s.tenant s.key st.receipts = none) (other : s.tenant ≠ tenant) :
    lookup tenant key (submit st s now true).state.receipts =
      lookup tenant key st.receipts := by
  simp [submit, unused, lookup, other]

#print axioms replay_original
#print axioms conflict_unchanged
#print axioms refusal_key_free
#print axioms accepted_once
#print axioms accept_then_replay
#print axioms repeat_replay_unchanged
#print axioms repeat_replay_length
#print axioms other_tenant_unchanged

end KernelProofs

```

