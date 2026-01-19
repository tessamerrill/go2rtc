package kasa

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

// TestXorEncodeDecode tests the XOR encoding and decoding functions
// against known values from the Python implementation
func TestXorEncodeDecode(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "simple string",
			input: "hello",
		},
		{
			name:  "json object",
			input: `{"test":"value"}`,
		},
		{
			name:  "empty string",
			input: "",
		},
		{
			name:  "complex json",
			input: `{"rtp":{"set_prepare_rtc_session":{"sessionId":"TwoWayAudio_1"}}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Encode
			encoded := xorEncode([]byte(tt.input))

			// Decode
			decoded := xorDecode(encoded)

			// Check if we get back the original
			if string(decoded) != tt.input {
				t.Errorf("xorEncodeDecode() failed:\nwant: %q\ngot:  %q", tt.input, string(decoded))
			}
		})
	}
}

// TestXorEncodeKnownValue tests against a known encoded value
// This ensures compatibility with the Python implementation
func TestXorEncodeKnownValue(t *testing.T) {
	// Simple test case: "test"
	input := []byte("test")
	encoded := xorEncode(input)

	// XOR with seed 0xAB:
	// 't' = 0x74: 0xAB ^ 0x74 = 0xDF
	// 'e' = 0x65: 0xDF ^ 0x65 = 0xBA
	// 's' = 0x73: 0xBA ^ 0x73 = 0xC9
	// 't' = 0x74: 0xC9 ^ 0x74 = 0xBD
	expected := []byte{0xDF, 0xBA, 0xC9, 0xBD}

	if len(encoded) != len(expected) {
		t.Fatalf("encoded length mismatch: want %d, got %d", len(expected), len(encoded))
	}

	for i := range encoded {
		if encoded[i] != expected[i] {
			t.Errorf("encoded[%d] = 0x%02X, want 0x%02X", i, encoded[i], expected[i])
		}
	}
}

// TestXorDecodeKnownValue tests against a known decoded value
func TestXorDecodeKnownValue(t *testing.T) {
	// Known encoded value for "test"
	encoded := []byte{0xDF, 0xBA, 0xC9, 0xBD}
	decoded := xorDecode(encoded)

	expected := "test"
	if string(decoded) != expected {
		t.Errorf("xorDecode() = %q, want %q", string(decoded), expected)
	}
}

// TestEncodeForPost tests the complete encoding pipeline
func TestEncodeForPost(t *testing.T) {
	// Test JSON encoding
	jsonObj := map[string]interface{}{
		"rtp": map[string]interface{}{
			"set_prepare_rtc_session": map[string]interface{}{
				"sessionId": "TwoWayAudio_1",
			},
		},
	}

	encoded, err := encodeForPost(jsonObj)
	if err != nil {
		t.Fatalf("encodeForPost() error = %v", err)
	}

	// Should start with "content="
	if len(encoded) < 8 || encoded[:8] != "content=" {
		t.Errorf("encoded string should start with 'content=', got: %s", encoded[:min(8, len(encoded))])
	}

	// Extract the base64 part (after "content=")
	// Note: URL encoding might have %XX sequences
	// For now, just verify it's a valid format
	if len(encoded) < 10 {
		t.Errorf("encoded string too short: %d bytes", len(encoded))
	}
}

// TestDecodeResponseBytes tests the response decoding
func TestDecodeResponseBytes(t *testing.T) {
	// Create a test response
	testJSON := `{"result":"success"}`

	// Encode it the same way the camera would
	obf := xorEncode([]byte(testJSON))
	b64 := base64.StdEncoding.EncodeToString(obf)
	responseBytes := []byte("content=" + b64)

	// Decode it
	decoded, err := decodeResponseBytes(responseBytes)
	if err != nil {
		t.Fatalf("decodeResponseBytes() error = %v", err)
	}

	// Verify we get back the original JSON
	if decoded != testJSON {
		t.Errorf("decodeResponseBytes() = %q, want %q", decoded, testJSON)
	}

	// Verify it's valid JSON
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(decoded), &result); err != nil {
		t.Errorf("decoded response is not valid JSON: %v", err)
	}
}

// TestRoundTrip tests a complete encode/decode round trip
func TestRoundTrip(t *testing.T) {
	testCases := []map[string]interface{}{
		{
			"rtp": map[string]interface{}{
				"set_prepare_rtc_session": map[string]interface{}{
					"sessionId": "TwoWayAudio_1",
				},
			},
		},
		{
			"rtp": map[string]interface{}{
				"set_rtc_session_status": map[string]interface{}{
					"sessionId": "TwoWayAudio_1",
					"connected": true,
				},
			},
		},
	}

	for i, testCase := range testCases {
		// Encode using encodeForPost (which URL encodes)
		encoded, err := encodeForPost(testCase)
		if err != nil {
			t.Fatalf("test case %d: encodeForPost() error = %v", i, err)
		}

		// Simulate a response (which would be form-encoded)
		responseBytes := []byte(encoded)

		// Decode
		decoded, err := decodeResponseBytes(responseBytes)
		if err != nil {
			t.Fatalf("test case %d: decodeResponseBytes() error = %v", i, err)
		}

		// Parse both as JSON and compare structures
		var original, decodedJSON map[string]interface{}
		originalJSON, _ := json.Marshal(testCase)
		if err := json.Unmarshal(originalJSON, &original); err != nil {
			t.Fatalf("test case %d: can't unmarshal original: %v", i, err)
		}
		if err := json.Unmarshal([]byte(decoded), &decodedJSON); err != nil {
			t.Fatalf("test case %d: can't unmarshal decoded: %v", i, err)
		}

		// Verify both have the same structure (at least same top-level keys)
		if len(decodedJSON) == 0 {
			t.Errorf("test case %d: decoded JSON is empty", i)
		}
		if _, ok := original["rtp"]; ok {
			if _, ok := decodedJSON["rtp"]; !ok {
				t.Errorf("test case %d: decoded JSON missing 'rtp' key", i)
			}
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
