package usecase

import (
	"encoding/json"
	"testing"
)

func TestApplyCuratorConfigDefaults(t *testing.T) {
	unmarshal := func(t *testing.T, b []byte) map[string]any {
		t.Helper()
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		return m
	}
	curatorEnabled := func(m map[string]any) (bool, bool) {
		curator, ok := m["curator"].(map[string]any)
		if !ok {
			return false, false
		}
		v, ok := curator["enabled"].(bool)
		return v, ok
	}

	t.Run("empty config gets curator disabled", func(t *testing.T) {
		out, err := applyCuratorConfigDefaults([]byte(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		v, ok := curatorEnabled(unmarshal(t, out))
		if !ok {
			t.Fatal("curator.enabled not set")
		}
		if v {
			t.Error("expected curator.enabled=false, got true")
		}
	})

	t.Run("unrelated config gets curator disabled", func(t *testing.T) {
		out, err := applyCuratorConfigDefaults([]byte(`{"model":{"provider":"anthropic"}}`))
		if err != nil {
			t.Fatal(err)
		}
		v, ok := curatorEnabled(unmarshal(t, out))
		if !ok {
			t.Fatal("curator.enabled not set")
		}
		if v {
			t.Error("expected curator.enabled=false, got true")
		}
	})

	t.Run("existing curator.enabled true is preserved", func(t *testing.T) {
		out, err := applyCuratorConfigDefaults([]byte(`{"curator":{"enabled":true}}`))
		if err != nil {
			t.Fatal(err)
		}
		v, ok := curatorEnabled(unmarshal(t, out))
		if !ok {
			t.Fatal("curator.enabled not set")
		}
		if !v {
			t.Error("expected curator.enabled=true to be preserved, got false")
		}
	})

	t.Run("curator block without enabled key gets enabled false", func(t *testing.T) {
		out, err := applyCuratorConfigDefaults([]byte(`{"curator":{"interval":"1h"}}`))
		if err != nil {
			t.Fatal(err)
		}
		m := unmarshal(t, out)
		v, ok := curatorEnabled(m)
		if !ok {
			t.Fatal("curator.enabled not set")
		}
		if v {
			t.Error("expected curator.enabled=false, got true")
		}
		curator := m["curator"].(map[string]any)
		if curator["interval"] != "1h" {
			t.Errorf("existing curator field mutated: %v", curator)
		}
	})
}
