package rakuten

import (
	"testing"
)

func TestGenerateSignature_GET(t *testing.T) {
	secret := "test-secret-key"
	nonce := "1586345939000"
	uri := "/api/v1/ticker"
	queryString := "symbolId=7"

	sig := GenerateSignatureForGET(secret, nonce, uri, queryString)

	if sig == "" {
		t.Fatal("signature should not be empty")
	}

	sig2 := GenerateSignatureForGET(secret, nonce, uri, queryString)
	if sig != sig2 {
		t.Fatalf("signatures should be deterministic: got %s and %s", sig, sig2)
	}
}

func TestGenerateSignature_GET_NoQuery(t *testing.T) {
	secret := "test-secret-key"
	nonce := "1586345939000"
	uri := "/api/v1/asset"

	sig := GenerateSignatureForGET(secret, nonce, uri, "")

	if sig == "" {
		t.Fatal("signature should not be empty")
	}
}

func TestGenerateSignature_POST(t *testing.T) {
	secret := "test-secret-key"
	nonce := "1586345939000"
	body := `{"symbolId":7,"orderPattern":"NORMAL"}`

	sig := GenerateSignatureForPOST(secret, nonce, body)

	if sig == "" {
		t.Fatal("signature should not be empty")
	}

	sig2 := GenerateSignatureForPOST(secret, nonce, body)
	if sig != sig2 {
		t.Fatalf("signatures should be deterministic: got %s and %s", sig, sig2)
	}
}

func TestGenerateSignature_DifferentInputs(t *testing.T) {
	secret := "test-secret-key"

	sig1 := GenerateSignatureForGET(secret, "1000", "/api/v1/ticker", "symbolId=7")
	sig2 := GenerateSignatureForGET(secret, "2000", "/api/v1/ticker", "symbolId=7")

	if sig1 == sig2 {
		t.Fatal("different nonces should produce different signatures")
	}
}

func TestGenerateHeaders(t *testing.T) {
	apiKey := "my-api-key"
	apiSecret := "my-api-secret"

	headers := GenerateAuthHeaders(apiKey, apiSecret, "GET", "/api/v1/ticker", "symbolId=7", "")

	if headers["API-KEY"] != apiKey {
		t.Fatalf("API-KEY should be %s, got %s", apiKey, headers["API-KEY"])
	}

	if headers["NONCE"] == "" {
		t.Fatal("NONCE should not be empty")
	}

	if headers["SIGNATURE"] == "" {
		t.Fatal("SIGNATURE should not be empty")
	}
}
