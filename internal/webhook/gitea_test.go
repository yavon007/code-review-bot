package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestVerifyGiteaSignature(t *testing.T) {
	body := []byte(`{"ok":true}`)
	secret := "test-secret"
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	signature := hex.EncodeToString(mac.Sum(nil))

	if !VerifyGiteaSignature(secret, body, signature) {
		t.Fatal("expected signature to be valid")
	}
	if !VerifyGiteaSignature(secret, body, "sha256="+signature) {
		t.Fatal("expected sha256-prefixed signature to be valid")
	}
	if VerifyGiteaSignature(secret, body, "bad-signature") {
		t.Fatal("expected malformed signature to be invalid")
	}
	if VerifyGiteaSignature("", body, signature) {
		t.Fatal("expected empty secret to be invalid")
	}
}

func TestDecodePayload(t *testing.T) {
	body := []byte(`{
		"action":"opened",
		"repository":{
			"full_name":"acme/order-service",
			"name":"order-service",
			"owner":{"username":"acme"}
		},
		"pull_request":{
			"index":123,
			"title":"Fix race",
			"head":{"ref":"feature/race","sha":"abc123"},
			"base":{"ref":"main","sha":"def456"}
		},
		"sender":{"username":"alice"}
	}`)

	input, err := DecodePayload("pull_request", "delivery-1", body)
	if err != nil {
		t.Fatalf("DecodePayload returned error: %v", err)
	}

	if input.DeliveryID != "delivery-1" {
		t.Fatalf("unexpected delivery id: %s", input.DeliveryID)
	}
	if input.RepoFullName != "acme/order-service" {
		t.Fatalf("unexpected repo full name: %s", input.RepoFullName)
	}
	if input.Owner != "acme" || input.Repo != "order-service" {
		t.Fatalf("unexpected repo parts: %s/%s", input.Owner, input.Repo)
	}
	if input.PRNumber != 123 {
		t.Fatalf("unexpected pr number: %d", input.PRNumber)
	}
	if input.HeadSHA != "abc123" || input.BaseSHA != "def456" {
		t.Fatalf("unexpected shas: %s/%s", input.HeadSHA, input.BaseSHA)
	}
	if input.Sender != "alice" {
		t.Fatalf("unexpected sender: %s", input.Sender)
	}
}

func TestDecodePayloadUnsupportedEvent(t *testing.T) {
	_, err := DecodePayload("push", "delivery-1", []byte(`{}`))
	if err != ErrUnsupportedEvent {
		t.Fatalf("expected ErrUnsupportedEvent, got %v", err)
	}
}
