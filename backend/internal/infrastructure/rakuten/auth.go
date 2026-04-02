package rakuten

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

// GenerateSignatureForGET generates SIGNATURE for GET/DELETE requests.
// Signs: NONCE + URI + queryString
func GenerateSignatureForGET(secret, nonce, uri, queryString string) string {
	message := nonce + uri
	if queryString != "" {
		message += "?" + queryString
	}
	return computeHMACSHA256(secret, message)
}

// GenerateSignatureForPOST generates SIGNATURE for POST/PUT requests.
// Signs: NONCE + JSON body
func GenerateSignatureForPOST(secret, nonce, jsonBody string) string {
	message := nonce + jsonBody
	return computeHMACSHA256(secret, message)
}

// GenerateAuthHeaders generates the 3 auth headers required by Rakuten Wallet API.
func GenerateAuthHeaders(apiKey, apiSecret, method, uri, queryString, jsonBody string) map[string]string {
	nonce := fmt.Sprintf("%d", time.Now().UnixMilli())

	var signature string
	switch method {
	case "POST", "PUT":
		signature = GenerateSignatureForPOST(apiSecret, nonce, jsonBody)
	default:
		signature = GenerateSignatureForGET(apiSecret, nonce, uri, queryString)
	}

	return map[string]string{
		"API-KEY":   apiKey,
		"NONCE":     nonce,
		"SIGNATURE": signature,
	}
}

func computeHMACSHA256(secret, message string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}
