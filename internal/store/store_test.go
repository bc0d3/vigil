package store

import (
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) (Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "vigil.db")
	s, err := Open(Config{SQLitePath: path})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, path
}

func obs(url, sha string) Observation {
	return Observation{URL: url, SHA256: sha, Status: 200, Size: 10, ContentType: "text/html", At: time.Now()}
}

// primera vez = new; mismo hash = unchanged; hash distinto = changed con previo.
func TestObserveLifecycle(t *testing.T) {
	s, _ := newTestStore(t)
	u := "https://x/main.js"

	c, err := s.Observe(obs(u, "aaa"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Status != StatusNew {
		t.Errorf("primera vez: status = %q, want new", c.Status)
	}

	c, err = s.Observe(obs(u, "aaa"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Status != StatusUnchanged {
		t.Errorf("mismo hash: status = %q, want unchanged", c.Status)
	}
	if c.PreviousSHA256 != "aaa" {
		t.Errorf("previous = %q, want aaa", c.PreviousSHA256)
	}

	c, err = s.Observe(obs(u, "bbb"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Status != StatusChanged {
		t.Errorf("hash distinto: status = %q, want changed", c.Status)
	}
	if c.PreviousSHA256 != "aaa" {
		t.Errorf("previous = %q, want aaa (el hash anterior)", c.PreviousSHA256)
	}
}

// el estado sobrevive a reabrir el archivo (es una DB de verdad).
func TestPersistenceAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vigil.db")
	u := "https://x/a.js"

	s1, err := Open(Config{SQLitePath: path})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s1.Observe(obs(u, "h1")); err != nil {
		t.Fatal(err)
	}
	_ = s1.Close()

	s2, err := Open(Config{SQLitePath: path})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s2.Close() }()

	c, err := s2.Observe(obs(u, "h1"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Status != StatusUnchanged {
		t.Errorf("tras reabrir, mismo hash debería ser unchanged, got %q", c.Status)
	}
}

// dos URLs distintas no se pisan.
func TestSeparateURLs(t *testing.T) {
	s, _ := newTestStore(t)
	if c, _ := s.Observe(obs("https://a/x", "1")); c.Status != StatusNew {
		t.Errorf("a: %q", c.Status)
	}
	if c, _ := s.Observe(obs("https://b/x", "2")); c.Status != StatusNew {
		t.Errorf("b: %q", c.Status)
	}
	if c, _ := s.Observe(obs("https://a/x", "1")); c.Status != StatusUnchanged {
		t.Errorf("a otra vez: %q", c.Status)
	}
}

func TestDomainOf(t *testing.T) {
	cases := map[string]string{
		"https://sub.example.com/a/b.js?q=1": "sub.example.com",
		"http://example.com:8080/x":          "example.com",
		"not a url":                          "not a url",
	}
	for in, want := range cases {
		if got := domainOf(in); got != want {
			t.Errorf("domainOf(%q) = %q, want %q", in, got, want)
		}
	}
}
