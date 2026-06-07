package acme

import "testing"

func TestChallengeStore(t *testing.T) {
	cs := NewChallengeStore()

	vals := cs.GetTXT("_acme-challenge.example.com.")
	if len(vals) != 0 {
		t.Error("expected empty for empty store")
	}

	cs.Set("_acme-challenge.example.com.", "value-1")
	cs.Set("_acme-challenge.example.com.", "value-2")

	vals = cs.GetTXT("_acme-challenge.example.com.")
	if len(vals) != 2 {
		t.Fatalf("expected 2 values, got %d", len(vals))
	}

	if vals[0] != "value-1" || vals[1] != "value-2" {
		t.Errorf("values = %v, want [value-1, value-2]", vals)
	}

	cs.Delete("_acme-challenge.example.com.", "value-1")

	vals = cs.GetTXT("_acme-challenge.example.com.")
	if len(vals) != 1 || vals[0] != "value-2" {
		t.Errorf("after delete: values = %v, want [value-2]", vals)
	}

	cs.Delete("_acme-challenge.example.com.", "value-2")

	vals = cs.GetTXT("_acme-challenge.example.com.")
	if len(vals) != 0 {
		t.Error("expected empty after deleting all")
	}
}
