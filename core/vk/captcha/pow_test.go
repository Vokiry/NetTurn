package captcha

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

func TestSolvePoW(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	powInput := "vk_test_pow_input_12345_"
	difficulty := 3 // 3 ведущих нуля

	hash, err := SolvePoW(ctx, powInput, difficulty)
	if err != nil {
		t.Fatalf("SolvePoW failed: %v", err)
	}

	target := strings.Repeat("0", difficulty)
	if !strings.HasPrefix(hash, target) {
		t.Fatalf("PoW hash %q does not start with %q", hash, target)
	}
}

func TestParseBootstrap(t *testing.T) {
	htmlSample := `
<!DOCTYPE html>
<html>
<head>
<script>
window.__initialData = {
    "pow_input": "abc123xyz789",
    "difficulty": 4,
    "domain": "vk.com"
};
</script>
</head>
<body></body>
</html>`

	data, err := ParseBootstrap(htmlSample)
	if err != nil {
		t.Fatalf("ParseBootstrap failed: %v", err)
	}

	if data.PowInput != "abc123xyz789" {
		t.Errorf("Expected PowInput 'abc123xyz789', got %q", data.PowInput)
	}
	if data.Difficulty != 4 {
		t.Errorf("Expected Difficulty 4, got %d", data.Difficulty)
	}
}

func TestGenerateFakeCursor(t *testing.T) {
	cursorJSON := GenerateFakeCursor()
	if !strings.HasPrefix(cursorJSON, "[") || !strings.HasSuffix(cursorJSON, "]") {
		t.Fatalf("Fake cursor must be a JSON array: %s", cursorJSON)
	}
	if !strings.Contains(cursorJSON, `"x":`) || !strings.Contains(cursorJSON, `"y":`) {
		t.Fatalf("Fake cursor must contain x and y coordinates: %s", cursorJSON)
	}
}

func TestPoWVerification(t *testing.T) {
	// Проверяем, что результат действительно является валидным SHA256 хешем
	ctx := context.Background()
	input := "benchmark_test_"
	diff := 2
	h, err := SolvePoW(ctx, input, diff)
	if err != nil {
		t.Fatalf("SolvePoW error: %v", err)
	}
	if len(h) != 64 {
		t.Fatalf("SHA256 hex string must be 64 characters")
	}

	// Декодируем и проверяем, что это валидный hex
	decoded, err := hex.DecodeString(h)
	if err != nil {
		t.Fatalf("Hash is not valid hex: %v", err)
	}
	if len(decoded) != sha256.Size {
		t.Fatalf("Decoded hash size must be 32 bytes")
	}
}
