package crypto

import "testing"

func TestAPIKeyHashDeterministic(t *testing.T) {
	k := "sk-some-random-key-123456"
	h1 := APIKeyHash(k)
	h2 := APIKeyHash(k)
	if h1 != h2 {
		t.Fatal("APIKeyHash not deterministic")
	}
	if APIKeyHash("different") == h1 {
		t.Fatal("different keys produced same hash")
	}
}

func TestPasswordRoundtrip(t *testing.T) {
	h, err := HashPassword("s3cret")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword(h, "s3cret") {
		t.Fatal("verify should pass")
	}
	if VerifyPassword(h, "wrong") {
		t.Fatal("verify should fail for wrong password")
	}
}

func TestCipherRoundtrip(t *testing.T) {
	master := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	c, err := NewCipher(master)
	if err != nil {
		t.Fatal(err)
	}
	secret := "sk-volcark-xxxxx"
	enc, err := c.Encrypt(secret)
	if err != nil {
		t.Fatal(err)
	}
	if enc == secret {
		t.Fatal("ciphertext equals plaintext")
	}
	dec, err := c.Decrypt(enc)
	if err != nil {
		t.Fatal(err)
	}
	if dec != secret {
		t.Fatalf("want %q got %q", secret, dec)
	}
}

func TestCipherRejectsBadMaster(t *testing.T) {
	if _, err := NewCipher("tooshort"); err == nil {
		t.Fatal("expected error for short master")
	}
}

func TestRandomHex(t *testing.T) {
	a, _ := RandomHex(16)
	b, _ := RandomHex(16)
	if len(a) != 32 || a == b {
		t.Fatal("random hex unexpected")
	}
}
