package postgres

import (
	"net/url"
	"testing"
)

func TestDatabaseURLWithPasswordEscapesSpecialCharacters(t *testing.T) {
	encoded, err := DatabaseURLWithPassword("postgres://nexdrop@postgres:5432/nexdrop?sslmode=disable", `p@ss:/?#[]!$&'()*+,;=%`)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(encoded)
	if err != nil {
		t.Fatal(err)
	}
	password, ok := parsed.User.Password()
	if !ok || password != `p@ss:/?#[]!$&'()*+,;=%` {
		t.Fatalf("password = %q, %v", password, ok)
	}
	if parsed.Host != "postgres:5432" {
		t.Fatalf("host = %q", parsed.Host)
	}
}
