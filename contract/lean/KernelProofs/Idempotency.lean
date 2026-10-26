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
