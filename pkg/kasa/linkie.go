package kasa

// LINKIE2 Protocol Implementation
//
// This file implements the Kasa LINKIE2 protocol for camera control and session management.
// The protocol uses XOR encoding with a seed value for obfuscating JSON commands sent to the camera.
//
// Protocol details:
//   - Port: 10443 (HTTPS)
//   - Authentication: HTTP Basic Auth
//   - Encoding: XOR with seed 0xAB, then Base64, then URL encoding
//   - Format: application/x-www-form-urlencoded with "content=" parameter
//
// Reference: https://github.com/tessamerrill/kasa-ptz-frigate/blob/main/tp_linkie_ptz.py

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// LINKIE2 protocol constants
const (
	LinkieSeed = 0xAB  // XOR encoding seed
	LinkiePort = 10443 // LINKIE API port
)

// xorEncode XORs the data bytes using LINKIE2 protocol
// Based on: https://github.com/tessamerrill/kasa-ptz-frigate/blob/main/tp_linkie_ptz.py#L54-61
func xorEncode(data []byte) []byte {
	arr := make([]byte, len(data))
	b := byte(LinkieSeed)
	for i := 0; i < len(data); i++ {
		b = b ^ data[i]
		arr[i] = b
	}
	return arr
}

// xorDecode decodes XOR encoded bytes using LINKIE2 protocol
// Based on: https://github.com/tessamerrill/kasa-ptz-frigate/blob/main/tp_linkie_ptz.py#L63-70
func xorDecode(data []byte) []byte {
	arr := make([]byte, len(data))
	prev := byte(LinkieSeed)
	for i := 0; i < len(data); i++ {
		cur := data[i]
		arr[i] = prev ^ cur
		prev = cur
	}
	return arr
}

// encodeForPost encodes a JSON object for HTTP POST to Kasa camera
// Based on: https://github.com/tessamerrill/kasa-ptz-frigate/blob/main/tp_linkie_ptz.py#L93-103
func encodeForPost(jsonObj map[string]interface{}) (string, error) {
	// Marshal JSON without spaces
	jsonBytes, err := json.Marshal(jsonObj)
	if err != nil {
		return "", err
	}

	// XOR encode
	obf := xorEncode(jsonBytes)

	// Base64 encode
	b64 := base64.StdEncoding.EncodeToString(obf)

	// URL encode with content= prefix
	encoded := "content=" + url.QueryEscape(b64)

	return encoded, nil
}

// decodeResponseBytes decodes camera response bytes
// Based on: https://github.com/tessamerrill/kasa-ptz-frigate/blob/main/tp_linkie_ptz.py#L105-115
func decodeResponseBytes(responseBytes []byte) (string, error) {
	// Parse response as form data to get content parameter
	values, err := url.ParseQuery(string(responseBytes))
	if err != nil {
		return "", err
	}

	contentB64 := values.Get("content")
	if contentB64 == "" {
		return "", fmt.Errorf("no content in response")
	}

	// Base64 decode
	decoded, err := base64.StdEncoding.DecodeString(contentB64)
	if err != nil {
		return "", err
	}

	// XOR decode
	jsonBytes := xorDecode(decoded)

	return string(jsonBytes), nil
}

// sendLinkieCommand sends a LINKIE command to the camera
// Based on: https://github.com/tessamerrill/kasa-ptz-frigate/blob/main/tp_linkie_ptz.py#L117-141
func sendLinkieCommand(ip string, command map[string]interface{}, username, password string) (string, error) {
	// Encode command
	encoded, err := encodeForPost(command)
	if err != nil {
		return "", err
	}

	// Create request
	linkieURL := fmt.Sprintf("https://%s:%d", ip, LinkiePort)
	req, err := http.NewRequest("POST", linkieURL, bytes.NewBufferString(encoded))
	if err != nil {
		return "", err
	}

	// Set headers
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Kasa_Android/3.4.9.1119")

	// Set basic auth
	req.SetBasicAuth(username, password)

	// Create HTTP client with TLS skip verify (Kasa cameras use self-signed certs)
	// NOTE: This is expected and required for Kasa cameras which do not have valid certificates
	// The camera is accessed via local IP on the user's network, not over the internet
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, // #nosec G402 - Required for Kasa cameras with self-signed certs
			},
		},
	}

	// Send request
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("linkie command failed: %s", resp.Status)
	}

	// Read response
	responseBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	// Decode response
	return decodeResponseBytes(responseBytes)
}
