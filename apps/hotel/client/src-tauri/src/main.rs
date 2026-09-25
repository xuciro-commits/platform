//! Hotel Desk: the Tauri client of the Hotel reference slice. Reservations are
//! drafted locally, queued in a persisted K5 outbox and sent to the tenant
//! server, which is the authority; drafts survive restarts and offline periods.

mod outbox;

use base64::{engine::general_purpose::STANDARD, Engine};
use outbox::{Authorities, Declaration, EntityRef, SchemaRef, State, Submission};
use serde_json::{json, Value};
use std::{path::PathBuf, sync::Mutex, time::Duration};
use tauri::{Manager, State as TauriState};

const EDGE: &str = "desk-app";
const DATA_CLASS: &str = "hotel.reservation";

#[derive(Default)]
struct App {
    server: String,
    token: String,
    tenant: String,
    principal: String,
    authorities: Authorities,
    file: PathBuf,
}

impl App {
    fn save(&self) -> Result<(), String> {
        let data = serde_json::to_vec_pretty(&self.authorities).map_err(|e| e.to_string())?;
        std::fs::write(&self.file, data).map_err(|e| e.to_string())
    }

    fn agent(&self) -> ureq::Agent {
        ureq::AgentBuilder::new().timeout(Duration::from_secs(3)).build()
    }

    fn get(&self, path: &str) -> Result<Value, String> {
        self.agent().get(&format!("{}{path}", self.server))
            .set("Authorization", &format!("Bearer {}", self.token))
            .call().map_err(|e| e.to_string())?
            .into_json().map_err(|e| e.to_string())
    }
}

impl App {
/// Takes the tenant's declarations from its authority (K5 A9).
fn refresh_declarations(&mut self) -> Result<(), String> {
    let declarations: Vec<Declaration> = serde_json::from_value(self.get("/v1/declarations")?).map_err(|e| e.to_string())?;
    self.authorities.refresh(declarations);
    Ok(())
}

fn authority(&self) -> String {
    self.authorities.authority_of(&self.tenant, DATA_CLASS).unwrap_or_default()
}

fn login(&mut self, server: String, token: String) -> Result<Value, String> {
    let app = self;
    app.server = server.trim_end_matches('/').to_string();
    app.token = token;
    let me = app.get("/v1/me")?;
    app.tenant = me["tenantId"].as_str().unwrap_or_default().to_string();
    app.principal = me["principalId"].as_str().unwrap_or_default().to_string();
    app.refresh_declarations()?;
    Ok(me)
}

/// Queues a decision: `schema` is create, modify or cancel; `payload` is its JSON.
fn draft(&mut self, schema: String, reservation_id: Option<String>, payload: Value, expected_revision: Option<u32>) -> Result<(), String> {
    let app = self;
    let id = reservation_id.unwrap_or_else(|| format!("res-{}", uuid::Uuid::new_v4()));
    let submission = Submission {
        tenant_id: app.tenant.clone(), principal_id: app.principal.clone(), authority: app.authority(),
        target: EntityRef { kind: DATA_CLASS.into(), id },
        schema: SchemaRef { name: format!("hotel.reservation.{schema}"), version: 1 },
        idempotency_key: uuid::Uuid::new_v4().to_string(),
        payload: STANDARD.encode(payload.to_string()),
        expected_revision,
        ..Default::default()
    };
    app.authorities.enqueue(submission).map_err(|e| e.code().to_string())?;
    app.save()
}

/// A user revision of a conflicting or rejected draft is a new submission (A6).
fn revise(&mut self, idempotency_key: String, payload: Value) -> Result<(), String> {
    let app = self;
    let mut submission = app.authorities.outbox.iter()
        .find(|e| e.submission.idempotency_key == idempotency_key && matches!(e.state, State::Conflict | State::Rejected))
        .ok_or("only conflicting or rejected drafts can be revised")?.submission.clone();
    submission.idempotency_key = uuid::Uuid::new_v4().to_string();
    submission.payload = STANDARD.encode(payload.to_string());
    app.authorities.enqueue(submission).map_err(|e| e.code().to_string())?;
    app.save()
}

/// Sends every pending draft and retries every unknown one with the same key (A5).
fn send(&mut self) -> Result<(), String> {
    let app = self;
    let due: Vec<(Submission, State)> = app.authorities.outbox.iter()
        .filter(|e| matches!(e.state, State::Pending | State::Unknown))
        .map(|e| (e.submission.clone(), e.state)).collect();
    for (s, state) in due {
        let (tenant, key) = (s.tenant_id.clone(), s.idempotency_key.clone());
        let start = if state == State::Pending { "send" } else { "retry" };
        app.authorities.transition(&tenant, &key, start).map_err(|e| e.code().to_string())?;
        app.save()?;
        let response = app.agent().post(&format!("{}/v1/submissions", app.server))
            .set("Authorization", &format!("Bearer {}", app.token))
            .send_json(serde_json::to_value(&s).unwrap());
        // K5 A8: the answer's error code decides the event; no answer is a timeout,
        // and a request that never left this edge goes back to PENDING.
        let (state, outcome) = match response {
            Ok(r) => {
                let id = r.into_json::<Value>().ok().and_then(|v| v["record"]["changeId"].as_str().map(String::from));
                (app.authorities.answer(&tenant, &key, None), id.unwrap_or_default())
            }
            Err(ureq::Error::Status(_, r)) => {
                let code = r.into_json::<Value>().ok().and_then(|v| v["error"]["code"].as_str().map(String::from));
                match code {
                    Some(code) => (app.authorities.answer(&tenant, &key, Some(&code)), code),
                    None => (app.authorities.transition(&tenant, &key, "timeout"), "no answer".into()),
                }
            }
            Err(ureq::Error::Transport(t)) if matches!(t.kind(), ureq::ErrorKind::ConnectionFailed | ureq::ErrorKind::Dns) =>
                (app.authorities.transition(&tenant, &key, "undelivered"), t.to_string()),
            Err(e) => (app.authorities.transition(&tenant, &key, "timeout"), e.to_string()),
        };
        state.map_err(|e| e.code().to_string())?;
        if outcome == "ERROR_CODE_NOT_AUTHORITY" {
            let _ = app.refresh_declarations();
        }
        if let Some(entry) = app.authorities.outbox.iter_mut().find(|e| e.submission.idempotency_key == key) {
            entry.outcome = outcome;
        }
        app.save()?;
    }
    Ok(())
}

/// The outbox (with decoded payloads) and, when reachable, the server's reservations and room types.
fn snapshot(&self) -> Value {
    let app = self;
    let outbox: Vec<Value> = app.authorities.outbox.iter().filter(|e| e.submission.tenant_id == app.tenant).map(|e| {
        let payload: Value = STANDARD.decode(&e.submission.payload).ok()
            .and_then(|b| serde_json::from_slice(&b).ok()).unwrap_or(Value::Null);
        json!({"key": e.submission.idempotency_key, "state": e.state, "schema": e.submission.schema.name,
               "reservation": e.submission.target.id, "payload": payload, "outcome": e.outcome})
    }).collect();
    // Records of the hotel's entity types (ADR-0016): the reservations and the room types a manager maintains.
    let records = |path: &str| if app.token.is_empty() { Ok(Value::Null) } else { app.get(path).map(|page| page["records"].clone()) };
    let reservations = records("/v1/records/hotel.reservation?sort=checkIn,id&archived=true&limit=500");
    let room_types = records("/v1/records/hotel.room-type?sort=name");
    json!({"principal": app.principal, "tenant": app.tenant, "outbox": outbox, "online": reservations.is_ok(),
           "reservations": reservations.unwrap_or(Value::Null), "roomTypes": room_types.unwrap_or(Value::Null)})
}
}

#[tauri::command]
fn login(app: TauriState<Mutex<App>>, server: String, token: String) -> Result<Value, String> {
    app.lock().unwrap().login(server, token)
}

#[tauri::command]
fn draft(app: TauriState<Mutex<App>>, schema: String, reservation_id: Option<String>, payload: Value, expected_revision: Option<u32>) -> Result<(), String> {
    app.lock().unwrap().draft(schema, reservation_id, payload, expected_revision)
}

#[tauri::command]
fn revise(app: TauriState<Mutex<App>>, idempotency_key: String, payload: Value) -> Result<(), String> {
    app.lock().unwrap().revise(idempotency_key, payload)
}

#[tauri::command]
fn send(app: TauriState<Mutex<App>>) -> Result<(), String> {
    app.lock().unwrap().send()
}

#[tauri::command]
fn snapshot(app: TauriState<Mutex<App>>) -> Value {
    app.lock().unwrap().snapshot()
}

fn main() {
    tauri::Builder::default()
        .setup(|tauri_app| {
            let dir = tauri_app.path().app_data_dir()?;
            std::fs::create_dir_all(&dir)?;
            let file = dir.join("outbox.json");
            let authorities = std::fs::read(&file).ok()
                .and_then(|data| serde_json::from_slice(&data).ok())
                .unwrap_or_else(|| Authorities::new(EDGE));
            tauri_app.manage(Mutex::new(App { authorities, file, ..Default::default() }));
            Ok(())
        })
        .invoke_handler(tauri::generate_handler![login, draft, revise, send, snapshot])
        .run(tauri::generate_context!())
        .expect("error while running Hotel Desk");
}

/// Reproduces the slice's flows against a running hotel-server started with
/// `-response-delay 4s -delay-count 1` (see apps/hotel/flows.sh).
#[cfg(test)]
mod flows {
    use super::*;

    fn state(app: &App, i: usize) -> String {
        app.snapshot()["outbox"][i]["state"].as_str().unwrap().replace("SUBMISSION_STATE_", "")
    }

    #[test]
    #[ignore = "needs a running hotel-server; run apps/hotel/flows.sh"]
    fn offline_timeout_conflict_and_rejection() {
        let server = std::env::var("HOTEL_SERVER").unwrap_or("http://127.0.0.1:8480".into());
        let dir = std::env::temp_dir().join(format!("hotel-desk-{}", uuid::Uuid::new_v4()));
        std::fs::create_dir_all(&dir).unwrap();
        let mut app = App { authorities: Authorities::new(EDGE), file: dir.join("outbox.json"), ..Default::default() };
        app.login(server.clone(), "desk-a".into()).unwrap();
        let stay = |guest: &str, check_in: &str, check_out: &str| json!({"roomType": "suite", "checkIn": check_in, "checkOut": check_out, "guest": guest});

        // 1. Timeout: the first response is delayed past the client timeout. The
        //    server applied it, the client cannot tell: UNKNOWN, then a retry
        //    with the same key returns the original record.
        app.draft("create".into(), None, stay("Ada", "2027-01-10", "2027-01-12"), Some(0)).unwrap();
        app.send().unwrap();
        assert_eq!(state(&app, 0), "UNKNOWN");
        app.send().unwrap();
        assert_eq!(state(&app, 0), "CONFIRMED");
        assert_eq!(app.snapshot()["reservations"].as_array().unwrap().len(), 1, "retry applied twice");

        // 2. Offline: drafts wait in the persisted outbox; a send that never
        //    left the edge goes back to PENDING (K5 undelivered) and is sent once online.
        app.draft("create".into(), None, stay("Grace", "2027-02-01", "2027-02-02"), Some(0)).unwrap();
        assert_eq!(state(&app, 1), "PENDING");
        let saved: Authorities = serde_json::from_slice(&std::fs::read(&app.file).unwrap()).unwrap();
        assert_eq!(saved.outbox.len(), 2, "draft not persisted");
        app.server = "http://127.0.0.1:9".into();
        app.send().unwrap();
        assert_eq!(state(&app, 1), "PENDING");
        app.server = server.clone();
        app.send().unwrap();
        assert_eq!(state(&app, 1), "CONFIRMED");

        // 3. Conflict: the only suite is taken; the draft is kept, never retried,
        //    and a user revision is a new submission.
        app.draft("create".into(), None, stay("Linus", "2027-01-11", "2027-01-13"), Some(0)).unwrap();
        app.send().unwrap();
        assert_eq!(state(&app, 2), "CONFLICT");
        app.send().unwrap();
        assert_eq!(state(&app, 2), "CONFLICT");
        let key = app.snapshot()["outbox"][2]["key"].as_str().unwrap().to_string();
        app.revise(key, stay("Linus", "2027-01-20", "2027-01-22")).unwrap();
        app.send().unwrap();
        assert_eq!(state(&app, 3), "CONFIRMED");

        // 4. Rejection: front desk may not cancel; the draft stays REJECTED.
        let id = app.snapshot()["reservations"][0]["id"].as_str().unwrap().to_string();
        app.draft("cancel".into(), Some(id), json!({}), Some(1)).unwrap();
        app.send().unwrap();
        assert_eq!(state(&app, 4), "REJECTED");
        assert_eq!(app.snapshot()["outbox"][4]["outcome"], "ERROR_CODE_POLICY_DENIED");
        std::fs::remove_dir_all(dir).ok();
    }
}
