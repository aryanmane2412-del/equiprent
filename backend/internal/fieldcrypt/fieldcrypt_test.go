package fieldcrypt

import (
	"bytes"
	"testing"
)

const key = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="

func TestSealAndOpen(t *testing.T) {
	c, err := New(key, "hash-key")
	if err != nil {
		t.Fatal(err)
	}
	s, err := c.Seal("pan", " abcde-1234f ")
	if err != nil || s.Last4 != "234F" {
		t.Fatalf("seal: %+v %v", s, err)
	}
	if bytes.Contains(s.Enc, []byte("ABCDE1234F")) {
		t.Fatal("plaintext visible in ciphertext")
	}
	plain, err := c.Decrypt("pan", s.Enc)
	if err != nil || plain != "ABCDE1234F" {
		t.Fatalf("decrypt = %q %v", plain, err)
	}
	if _, err := c.Decrypt("bank_account", s.Enc); err == nil {
		t.Fatal("ciphertext must be bound to its field")
	}
	again, _ := c.Seal("pan", "ABCDE1234F")
	if bytes.Equal(again.Enc, s.Enc) || !bytes.Equal(again.Hash, s.Hash) {
		t.Fatal("encryption must be randomised; the lookup hash deterministic")
	}
	if bytes.Equal(c.Hash("pan", "X"), c.Hash("licence", "X")) {
		t.Fatal("hashes must be field-separated")
	}
	if _, err := New("c2hvcnQ=", "k"); err == nil {
		t.Fatal("short key accepted")
	}
}
