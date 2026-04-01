package audit

import (
	"encoding/json"
	"testing"
)

func TestMask_NoFalsePositives(t *testing.T) {
	input := map[string]any{
		"pass_count": 42,
		"bypass":     true,
		"compass":    "north",
		"surpass":    "exceeded",
		"passenger":  "alice",
	}
	result := Mask(input)
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", result)
	}
	for key, val := range m {
		if val == "<redacted>" {
			t.Errorf("key %q should NOT be redacted but was", key)
		}
	}
}

func TestMask_TrueSensitiveKeysAreRedacted(t *testing.T) {
	input := map[string]any{
		"password":    "secret123",
		"passwd":      "letmein",
		"passphrase":  "correct horse battery",
		"api_key":     "key-xyz",
		"token":       "tok-abc",
		"credit_card": "4111111111111111",
		"private_key": "-----BEGIN RSA-----",
	}
	result := Mask(input)
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", result)
	}
	for key, val := range m {
		if val != "<redacted>" {
			t.Errorf("key %q should be redacted, got %v", key, val)
		}
	}
}

func TestMaskStructAndMap(t *testing.T) {
	type Nested struct {
		Secret string `json:"secret"`
		Note   string `json:"note"`
	}
	type User struct {
		ID       int    `json:"id"`
		Email    string `json:"email"`
		Password string `json:"password"`
		Token    string `json:"token"`
		Info     Nested `json:"info"`
	}

	u := User{ID: 1, Email: "a@b.com", Password: "pwd123", Token: "tok123", Info: Nested{Secret: "s", Note: "n"}}

	masked := Mask(u)
	b, err := json.Marshal(masked)
	if err != nil {
		t.Fatalf("marshal masked: %v", err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal masked: %v", err)
	}

	if out["password"] != "<redacted>" {
		t.Fatalf("password not redacted: %v", out["password"])
	}
	if out["token"] != "<redacted>" {
		t.Fatalf("token not redacted: %v", out["token"])
	}
	info, ok := out["info"].(map[string]interface{})
	if !ok {
		t.Fatalf("info not map: %T", out["info"])
	}
	if info["secret"] != "<redacted>" {
		t.Fatalf("nested secret not redacted: %v", info["secret"])
	}
	if info["note"] != "n" {
		t.Fatalf("note changed: %v", info["note"])
	}

	// test map
	m := map[string]any{"username": "bob", "password": "pwd"}
	maskedMap := Mask(m)
	bb, _ := json.Marshal(maskedMap)
	var out2 map[string]any
	err = json.Unmarshal(bb, &out2)
	if err != nil {
		t.Fatalf("unmarshal masked map: %v", err)
	}
	if out2["password"] != "<redacted>" {
		t.Fatalf("map password not redacted: %v", out2["password"])
	}
}
