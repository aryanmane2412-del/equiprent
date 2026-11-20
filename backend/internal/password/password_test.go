package password

import (
	"strings"
	"testing"
)

var fast = Params{MemoryKiB: 1024, Time: 1, Threads: 1, SaltLen: 16, KeyLen: 32}

func TestHashAndVerify(t *testing.T) {
	h, err := Hash("correct horse 42", Default)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Fatalf("hash = %s", h)
	}
	if ok, err := Verify("correct horse 42", h); !ok || err != nil {
		t.Fatalf("verify = %v %v", ok, err)
	}
	if ok, _ := Verify("correct horse 43", h); ok {
		t.Fatal("wrong password accepted")
	}
	h2, _ := Hash("correct horse 42", Default)
	if h == h2 {
		t.Fatal("hashes must be salted")
	}
}

func TestMalformedAndRehash(t *testing.T) {
	for _, bad := range []string{"", "plain", "$2a$10$bcrypt", "$argon2id$v=19$m=x$salt$key", "$argon2i$v=19$m=1,t=1,p=1$c2FsdHNhbHQ$a2V5a2V5a2V5a2V5a2V5"} {
		if _, err := Verify("x", bad); err == nil {
			t.Errorf("accepted malformed hash %q", bad)
		}
	}
	weak, _ := Hash("correct horse 42", fast)
	if !NeedsRehash(weak, Default) {
		t.Error("weaker parameters should need a rehash")
	}
	strong, _ := Hash("correct horse 42", Default)
	if NeedsRehash(strong, Default) {
		t.Error("current parameters need no rehash")
	}
	if ok, err := Verify("correct horse 42", weak); !ok || err != nil {
		t.Error("old parameters must still verify")
	}
}

func TestPolicy(t *testing.T) {
	ok := []string{"Tractor-Rental-2026", "correct horse 42", "kXz9!pQ2mL"}
	for _, p := range ok {
		if err := Check(p, "ravi@example.com", "9876543210"); err != nil {
			t.Errorf("%q rejected: %v", p, err)
		}
	}
	bad := map[string]string{
		"short1!":                "at least",
		strings.Repeat("a1", 70): "at most",
		"Password@123":           "too common",
		"aaaaaaaaa1":             "different characters",
		"onlylettersherex":       "mix letters",
		"1234567890123":          "mix letters",
		"ravi-is-the-best-1":     "name, email",
		"call9876543210now":      "name, email",
	}
	for p, want := range bad {
		err := Check(p, "Ravi", "ravi@example.com", "9876543210")
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: err = %v, want %q", p, err, want)
		}
	}
}
