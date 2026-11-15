package security

import (
	"bytes"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestVaultEncryptDecryptAndMask(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	v, err := NewVaultFromEnv(hex.EncodeToString(key))
	if err != nil {
		t.Fatalf("NewVaultFromEnv failed: %v", err)
	}

	secret := "sk-live-super-secret-token-9876"
	blob1, err := v.Encrypt(secret)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}
	blob2, err := v.Encrypt(secret)
	if err != nil {
		t.Fatalf("Encrypt 2 failed: %v", err)
	}
	if bytes.Equal(blob1, blob2) {
		t.Fatal("expected unique nonces to produce distinct ciphertexts")
	}

	got, err := v.Decrypt(blob1)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}
	if got != secret {
		t.Fatalf("got %q, want %q", got, secret)
	}

	// Tamper check
	tampered := append([]byte(nil), blob1...)
	tampered[len(tampered)-1] ^= 0xff
	if _, err := v.Decrypt(tampered); !errors.Is(err, ErrInvalidCiphertext) {
		t.Fatalf("expected ErrInvalidCiphertext on tampered blob, got %v", err)
	}

	if _, err := NewVaultFromEnv("too-short"); !errors.Is(err, ErrInvalidMasterKey) {
		t.Fatalf("expected ErrInvalidMasterKey, got %v", err)
	}

	if got := MaskLast4(secret); got != "****9876" {
		t.Fatalf("MaskLast4 = %q, want ****9876", got)
	}
	if got := MaskLast4("abc"); got != "****" {
		t.Fatalf("MaskLast4 short = %q, want ****", got)
	}
}

func TestPasswordHashAndVerify(t *testing.T) {
	if _, err := HashPassword("short"); !errors.Is(err, ErrPasswordPolicy) {
		t.Fatalf("expected ErrPasswordPolicy for short password, got %v", err)
	}

	pw := "CorrectHorseBatteryStaple!2026"
	hash, err := HashPassword(pw)
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$") {
		t.Fatalf("unexpected hash format: %s", hash)
	}

	ok, err := VerifyPassword(pw, hash)
	if err != nil || !ok {
		t.Fatalf("expected VerifyPassword to succeed, ok=%v err=%v", ok, err)
	}

	ok, err = VerifyPassword("WrongPassword123!", hash)
	if err != nil || ok {
		t.Fatalf("expected VerifyPassword to fail for wrong password, ok=%v err=%v", ok, err)
	}
}

func TestRedactingLogger(t *testing.T) {
	var buf bytes.Buffer
	logger := NewRedactingLogger(&buf)
	logger.Info("outbound call",
		"api_key", "sk-1234567890abcdef",
		"authorization", "Bearer supersecrettoken",
		"detail", "failed with Bearer mytoken123 and sk-abcdefghijkl",
	)
	out := buf.String()
	if strings.Contains(out, "1234567890abcdef") || strings.Contains(out, "supersecrettoken") || strings.Contains(out, "mytoken123") || strings.Contains(out, "abcdefghijkl") {
		t.Fatalf("secret leaked in log output: %s", out)
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Fatalf("expected [REDACTED] in log output: %s", out)
	}
}

func TestSSRFPolicyAndDialer(t *testing.T) {
	blocked := []string{
		"http://169.254.169.254/latest/meta-data/",
		"http://127.0.0.1:8080/admin",
		"http://localhost:3000",
		"http://10.0.0.5:8000",
		"http://192.168.1.1/api",
		"http://172.16.0.10/v1",
		"http://[::1]:8080/",
		"http://[fc00::1]/",
	}

	strict := SSRFPolicy{AllowPrivate: false}
	for _, raw := range blocked {
		if _, err := strict.ValidateHTTPURL(raw); err == nil {
			t.Fatalf("expected %s to be blocked when AllowPrivate=false", raw)
		}
	}

	permissive := SSRFPolicy{AllowPrivate: true}
	for _, raw := range blocked {
		if _, err := permissive.ValidateHTTPURL(raw); err != nil {
			t.Fatalf("expected %s to be allowed when AllowPrivate=true, got %v", raw, err)
		}
	}

	if !IsBlockedIP(net.ParseIP("169.254.169.254")) {
		t.Fatal("expected metadata IP to be blocked")
	}

	// Verify Dialer Control hook blocks loopback at connection time even if URL check was bypassed
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	strictClient := NewSafeHTTPClient(false, 2*time.Second)
	if _, err := strictClient.Get(srv.URL); err == nil {
		t.Fatal("expected safe HTTP client to block loopback httptest server")
	}

	permissiveClient := NewSafeHTTPClient(true, 2*time.Second)
	resp, err := permissiveClient.Get(srv.URL)
	if err != nil {
		t.Fatalf("expected permissive client to succeed, got %v", err)
	}
	_ = resp.Body.Close()
}
