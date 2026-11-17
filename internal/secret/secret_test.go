package secret

import "testing"

func TestEncryptDecrypt(t *testing.T) {
	box, err := New("test-key-material")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := box.Encrypt("PHPSESSID=rahasia; cf_clearance=abc")
	if err != nil {
		t.Fatal(err)
	}
	if encoded == "" {
		t.Fatal("ciphertext kosong")
	}
	plain, err := box.Decrypt(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if plain != "PHPSESSID=rahasia; cf_clearance=abc" {
		t.Fatalf("plain = %q", plain)
	}
	encoded2, _ := box.Encrypt("PHPSESSID=rahasia; cf_clearance=abc")
	if encoded == encoded2 {
		t.Fatal("nonce harus acak")
	}
}

func TestWrongKeyAndTamper(t *testing.T) {
	box1, _ := New("key-one")
	box2, _ := New("key-two")
	encoded, _ := box1.Encrypt("secret")
	if _, err := box2.Decrypt(encoded); err == nil {
		t.Fatal("key berbeda seharusnya gagal")
	}
	box3, _ := New("key-one")
	tampered := encoded[:len(encoded)-2] + "AA"
	if _, err := box3.Decrypt(tampered); err == nil {
		t.Fatal("ciphertext rusak seharusnya gagal")
	}
}

func TestEmptyKey(t *testing.T) {
	if _, err := New("  "); err == nil {
		t.Fatal("key kosong harus error")
	}
}
