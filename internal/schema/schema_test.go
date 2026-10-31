package schema

import "testing"

func TestValidateObjectAndTypes(t *testing.T) {
	s, err := Compile([]byte(`{
		"type":"object",
		"additionalProperties":false,
		"required":["name","count"],
		"properties":{
			"name":{"type":"string","minLength":2},
			"count":{"type":"integer","minimum":1},
			"flag":{"type":"boolean"}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if iss := s.Validate(map[string]any{"name": "Ada", "count": float64(2)}); len(iss) != 0 {
		t.Fatalf("expected valid, got %+v", iss)
	}
	iss := s.Validate(map[string]any{"name": "A", "count": 1.5, "extra": true})
	if len(iss) != 3 {
		t.Fatalf("expected 3 issues, got %+v", iss)
	}
}

func TestApplyDefaultsAndEnums(t *testing.T) {
	s, err := Compile([]byte(`{
		"type":"object",
		"properties":{
			"mode":{"type":"string","enum":["a","b"],"default":"a"},
			"nested":{"type":"object","properties":{"n":{"type":"integer","default":3}}}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	got := s.ApplyDefaults(map[string]any{"nested": map[string]any{}})
	m := got.(map[string]any)
	if m["mode"] != "a" {
		t.Fatalf("default not applied: %+v", m)
	}
	if m["nested"].(map[string]any)["n"] != float64(3) && m["nested"].(map[string]any)["n"] != 3 {
		t.Fatalf("nested default not applied: %+v", m)
	}
	if iss := s.Validate(map[string]any{"mode": "c"}); len(iss) == 0 {
		t.Fatal("expected enum error")
	}
}

func TestIntegerAcceptsYAMLInt(t *testing.T) {
	s, _ := Compile([]byte(`{"type":"object","properties":{"n":{"type":"integer"}}}`))
	if iss := s.Validate(map[string]any{"n": 4}); len(iss) != 0 {
		t.Fatalf("int should be valid integer: %+v", iss)
	}
}
