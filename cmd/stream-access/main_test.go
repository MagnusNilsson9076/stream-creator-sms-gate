package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/infrai-examples/stream-creator-sms-gate/internal/media"
)

type fakeCodes struct {
	requested bool
	verified  bool
}

func (f *fakeCodes) RequestCode(context.Context, string, string) error {
	f.requested = true
	return nil
}

func (f *fakeCodes) VerifyCode(context.Context, string, string, string) error {
	f.verified = true
	return nil
}

func TestVerifiedPhoneUnlocksAssetIngestion(t *testing.T) {
	codes := &fakeCodes{}
	app := &service{sms: codes, pipeline: media.NewPipeline(), sessions: make(map[string]string)}

	call := func(handler http.HandlerFunc, body, token string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
		req.Header.Set("Idempotency-Key", "login-attempt-42")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		res := httptest.NewRecorder()
		handler(res, req)
		return res
	}

	request := call(app.requestCode, `{"phone":"+14155550100"}`, "")
	if request.Code != http.StatusAccepted || !codes.requested {
		t.Fatalf("request code status = %d, called = %v", request.Code, codes.requested)
	}

	assetBody := `{"id":"asset-42","creator":"creator-9","title":"Episode 12"}`
	blocked := call(app.requireSession(app.ingestAsset), assetBody, "")
	if blocked.Code != http.StatusUnauthorized {
		t.Fatalf("asset before verification status = %d", blocked.Code)
	}

	verified := call(app.verifyCode, `{"phone":"+14155550100","code":"123456"}`, "")
	var login map[string]string
	if err := json.Unmarshal(verified.Body.Bytes(), &login); err != nil {
		t.Fatal(err)
	}
	if verified.Code != http.StatusOK || !codes.verified || login["session_token"] == "" {
		t.Fatalf("verification status = %d, called = %v, body = %s", verified.Code, codes.verified, verified.Body.String())
	}

	ingested := call(app.requireSession(app.ingestAsset), assetBody, login["session_token"])
	if ingested.Code != http.StatusOK {
		t.Fatalf("asset after verification status = %d, body = %s", ingested.Code, ingested.Body.String())
	}
}
