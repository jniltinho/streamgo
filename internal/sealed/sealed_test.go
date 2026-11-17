package sealed

import (
	"bytes"
	"errors"
	"testing"
)

func TestSeal(t *testing.T) {
	t.Parallel()
	box, err := New(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	text, err := box.Seal("invite", []byte("a-test-token"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(text, []byte("a-test-token")) {
		t.Fatal("sealed text holds the plain text")
	}
	if got, err := box.Open("invite", text); err != nil || string(got) != "a-test-token" {
		t.Fatalf("open: %q %v", got, err)
	}
	if again, _ := box.Seal("invite", []byte("a-test-token")); bytes.Equal(again, text) {
		t.Fatal("the nonce is not random")
	}
	other, _ := New(bytes.Repeat([]byte{8}, 32))
	for name, open := range map[string]func() ([]byte, error){
		"another bound": func() ([]byte, error) { return box.Open("destination", text) },
		"another key":   func() ([]byte, error) { return other.Open("invite", text) },
		"not sealed":    func() ([]byte, error) { return box.Open("invite", []byte("a-test-token")) },
		"short":         func() ([]byte, error) { return box.Open("invite", nil) },
	} {
		if got, err := open(); !errors.Is(err, ErrSealed) || got != nil {
			t.Errorf("%s: %q %v", name, got, err)
		}
	}
	if _, err := New(nil); err == nil {
		t.Fatal("a box without a key")
	}
}
