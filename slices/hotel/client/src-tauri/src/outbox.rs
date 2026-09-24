//! K5 Authority and sync for this edge (contract/spec/K5-authority.md):
//! declarations, the authority check, and the persisted outbox. Runs the
//! contract's K5 vectors, like the Go and Swift implementations.

use serde::{Deserialize, Serialize};
use std::collections::BTreeMap;

#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct EntityRef {
    #[serde(rename = "type")]
    pub kind: String,
    pub id: String,
}

#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct SchemaRef {
    pub name: String,
    pub version: u32,
}

/// A K4 submission in Protobuf JSON form; `payload` is base64.
#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct Submission {
    pub tenant_id: String,
    pub principal_id: String,
    pub authority: String,
    pub target: EntityRef,
    pub schema: SchemaRef,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub valid_time: Option<String>,
    #[serde(skip_serializing_if = "String::is_empty")]
    pub causation_id: String,
    #[serde(skip_serializing_if = "String::is_empty")]
    pub correlation_id: String,
    pub idempotency_key: String,
    pub payload: String,
}

#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct Declaration {
    pub tenant_id: String,
    pub data_class: String,
    pub kind: String,
    pub authority_id: String,
    pub epoch: u32,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub enum State {
    #[serde(rename = "SUBMISSION_STATE_PENDING")]
    Pending,
    #[serde(rename = "SUBMISSION_STATE_SENDING")]
    Sending,
    #[serde(rename = "SUBMISSION_STATE_CONFIRMED")]
    Confirmed,
    #[serde(rename = "SUBMISSION_STATE_CONFLICT")]
    Conflict,
    #[serde(rename = "SUBMISSION_STATE_REJECTED")]
    Rejected,
    #[serde(rename = "SUBMISSION_STATE_UNKNOWN")]
    Unknown,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
#[allow(dead_code)]
pub enum KernelError {
    InvalidArgument,
    NotFound,
    Conflict,
    IdempotencyConflict,
    NotAuthority,
}

impl KernelError {
    pub fn code(self) -> &'static str {
        match self {
            Self::InvalidArgument => "ERROR_CODE_INVALID_ARGUMENT",
            Self::NotFound => "ERROR_CODE_NOT_FOUND",
            Self::Conflict => "ERROR_CODE_CONFLICT",
            Self::IdempotencyConflict => "ERROR_CODE_IDEMPOTENCY_CONFLICT",
            Self::NotAuthority => "ERROR_CODE_NOT_AUTHORITY",
        }
    }
}

#[derive(Clone, Debug, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Entry {
    pub submission: Submission,
    pub state: State,
    /// The authority's answer: change ID or error code (display only).
    #[serde(default)]
    pub outcome: String,
}

/// Declarations, the receiver's check and this edge's outbox (A1–A7).
#[derive(Debug, Default, Serialize, Deserialize)]
pub struct Authorities {
    pub edge: String,
    current: BTreeMap<String, Declaration>, // "tenant\u{0}dataClass"
    pub outbox: Vec<Entry>,                  // in enqueue order, keyed by (tenant, key)
}

fn key(a: &str, b: &str) -> String {
    format!("{a}\u{0}{b}")
}

impl Authorities {
    pub fn new(edge: &str) -> Self {
        Self { edge: edge.into(), ..Default::default() }
    }

    pub fn declare(&mut self, d: Declaration) -> Result<(), KernelError> {
        if d.tenant_id.is_empty() || d.data_class.is_empty() || d.authority_id.is_empty()
            || d.kind.is_empty() || d.kind == "AUTHORITY_KIND_UNSPECIFIED"
        {
            return Err(KernelError::InvalidArgument); // A1
        }
        let k = key(&d.tenant_id, &d.data_class);
        if d.epoch != self.current.get(&k).map_or(0, |c| c.epoch) + 1 {
            return Err(KernelError::Conflict); // A2
        }
        self.current.insert(k, d);
        Ok(())
    }

    fn declaration(&self, s: &Submission) -> Result<&Declaration, KernelError> {
        self.current.get(&key(&s.tenant_id, &s.target.kind)).ok_or(KernelError::NotFound)
    }

    /// The receiver's check; an edge that is not the authority only runs it in the vectors.
    #[allow(dead_code)]
    pub fn authorize(&self, s: &Submission) -> Result<(), KernelError> {
        if self.declaration(s)?.authority_id != s.authority {
            return Err(KernelError::NotAuthority); // A3
        }
        Ok(())
    }

    fn entry(&mut self, tenant: &str, idempotency_key: &str) -> Option<&mut Entry> {
        self.outbox.iter_mut()
            .find(|e| e.submission.tenant_id == tenant && e.submission.idempotency_key == idempotency_key)
    }

    pub fn enqueue(&mut self, s: Submission) -> Result<State, KernelError> {
        let authority = self.declaration(&s)?.authority_id.clone();
        let edge = self.edge.clone();
        if let Some(existing) = self.entry(&s.tenant_id.clone(), &s.idempotency_key.clone()) {
            return if existing.submission == s { Ok(existing.state) } else { Err(KernelError::IdempotencyConflict) }; // A6
        }
        let state = if authority == edge { State::Confirmed } else { State::Pending }; // A4
        self.outbox.push(Entry { submission: s, state, outcome: String::new() });
        Ok(state)
    }

    pub fn transition(&mut self, tenant: &str, idempotency_key: &str, event: &str) -> Result<State, KernelError> {
        let entry = self.entry(tenant, idempotency_key).ok_or(KernelError::NotFound)?;
        let next = match (event, entry.state) {
            ("send", State::Pending) => State::Sending,
            ("confirm", State::Sending) => State::Confirmed,
            ("conflict", State::Sending) => State::Conflict,
            ("reject", State::Sending) => State::Rejected,
            ("timeout", State::Sending) => State::Unknown,
            ("retry", State::Unknown) => State::Sending,
            _ => return Err(KernelError::InvalidArgument), // A5
        };
        entry.state = next;
        Ok(next)
    }
}

#[cfg(test)]
mod conformance {
    use super::*;
    use serde_json::Value;

    #[test]
    fn k5_vectors() {
        let path = concat!(env!("CARGO_MANIFEST_DIR"), "/../../../../contract/vectors/k5-authority.json");
        let file: Value = serde_json::from_str(&std::fs::read_to_string(path).unwrap()).unwrap();
        assert_eq!(file["contract"], "v1alpha1");
        let mut steps = 0;
        for vector in file["vectors"].as_array().unwrap() {
            let id = vector["id"].as_str().unwrap();
            let mut a = Authorities::new(vector["given"]["edge"].as_str().unwrap());
            for d in vector["given"]["declarations"].as_array().unwrap() {
                a.declare(serde_json::from_value(d.clone()).unwrap()).unwrap();
            }
            for (i, step) in vector["steps"].as_array().unwrap().iter().enumerate() {
                let sub = |k: &str| serde_json::from_value::<Submission>(step[k].clone()).unwrap();
                let result: Result<Option<State>, KernelError> = if !step["declare"].is_null() {
                    a.declare(serde_json::from_value(step["declare"].clone()).unwrap()).map(|_| None)
                } else if !step["authorize"].is_null() {
                    a.authorize(&sub("authorize")).map(|_| None)
                } else if !step["enqueue"].is_null() {
                    a.enqueue(sub("enqueue")).map(Some)
                } else {
                    let t = &step["transition"];
                    a.transition(t["tenantId"].as_str().unwrap(), t["idempotencyKey"].as_str().unwrap(),
                                 t["event"].as_str().unwrap()).map(Some)
                };
                let got = match result {
                    Ok(None) => serde_json::json!({"ok": true}),
                    Ok(Some(state)) => serde_json::json!({"state": state}),
                    Err(e) => serde_json::json!({"error": e.code()}),
                };
                assert_eq!(got, step["expect"], "{id} step {i}");
                steps += 1;
            }
        }
        assert!(steps > 0);
    }
}
