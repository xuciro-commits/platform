package data

import (
	"io"
	"net/http"
	"testing"
)

func TestConnectorHMACFixedVector(t *testing.T) {
	req := WritebackRequest{Consumer: "implementation/build:fixture", Method: http.MethodPost, URL: "https://api.example.test/v1/calls?region=us", Auth: "hmac-sha256", Body: map[string]any{"destination": "+15550100"}}
	r, err := req.HTTPRequest("fixture-call-1", []byte("kit-test-key"))
	if err != nil {
		t.Fatal(err)
	}
	const want = "sha256=7b73152a2a855cd477688d0dd6815a22b6e81958c303081008058d38341344f4"
	body, err := io.ReadAll(r.Body)
	if err != nil || string(body) != `{"destination":"+15550100"}` || r.Header.Get("X-Platform-Signature") != want || r.Header.Get("Authorization") != "" {
		t.Fatal("signed request does not match the fixed vector")
	}
	if !connectorAnswerSensitive([]byte(`{"secret":"kit-test-key"}`), r, []byte("kit-test-key")) || !connectorAnswerSensitive([]byte(want), r, []byte("kit-test-key")) {
		t.Fatal("credential material must not be retained")
	}
	req.Auth = "basic"
	r, err = req.HTTPRequest("fixture-call-1", []byte("fixture-user:fixture-pass"))
	if err != nil {
		t.Fatal(err)
	}
	user, password, ok := r.BasicAuth()
	if !ok || user != "fixture-user" || password != "fixture-pass" || r.Header.Get("X-Platform-Signature") != "" {
		t.Fatal("Basic authentication differs")
	}
	if !connectorAnswerSensitive([]byte(`{"password":"fixture-pass"}`), r, []byte("fixture-user:fixture-pass")) {
		t.Fatal("password must not be retained")
	}
}
