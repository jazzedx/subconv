package converter

import (
	"fmt"
	"testing"
)

func TestParseVLESSReality(t *testing.T) {
	raw := `vless://d5d4b021-5cae-4e56-9429-058655ab2adf@px.the-jazzed.com:443?type=tcp&encryption=none&security=reality&pbk=EVCufy38gXSsxeNQcFSf4hUBvok0J_Q-d3WgxnEeyjI&fp=chrome&sni=www.nvidia.com&sid=94&spx=%2F&flow=xtls-rprx-vision#Vless%20Reality-ba1d6x7lc`

	proxies := ParseContent([]byte(raw), "test")
	if len(proxies) != 1 {
		t.Fatalf("expected 1 proxy, got %d", len(proxies))
	}

	p := proxies[0]
	if p.Type != "vless" {
		t.Errorf("type = %s, want vless", p.Type)
	}
	if p.RealityPublicKey != "EVCufy38gXSsxeNQcFSf4hUBvok0J_Q-d3WgxnEeyjI" {
		t.Errorf("public key = %s", p.RealityPublicKey)
	}
	if p.RealityShortID != "94" {
		t.Errorf("short-id = %q, want %q", p.RealityShortID, "94")
	}
	if p.Flow != "xtls-rprx-vision" {
		t.Errorf("flow = %s", p.Flow)
	}
	if p.Name != "Vless Reality-ba1d6x7lc" {
		t.Errorf("name = %s", p.Name)
	}
	if p.Server != "px.the-jazzed.com" {
		t.Errorf("server = %s", p.Server)
	}
	if p.Port != 443 {
		t.Errorf("port = %d", p.Port)
	}

	// Test Clash output
	m := ProxyToMap(p)
	realityOpts, ok := m["reality-opts"].(map[string]any)
	if !ok {
		t.Fatal("reality-opts not found")
	}
	sid := fmt.Sprintf("%v", realityOpts["short-id"])
	if sid != "94" {
		t.Errorf("clash short-id = %q, want %q", sid, "94")
	}

	// Test full YAML generation
	yaml, err := GenerateClashConfig(proxies)
	if err != nil {
		t.Fatal(err)
	}
	yamlStr := string(yaml)
	if !contains(yamlStr, `short-id: "94"`) {
		t.Errorf("YAML does not contain short-id: \"94\"\n%s", yamlStr)
	}
	t.Logf("Generated YAML:\n%s", yamlStr)
}

func TestParseVMess(t *testing.T) {
	// Standard vmess link
	raw := `vmess://eyJ2IjoiMiIsInBzIjoidGVzdC1ub2RlIiwiYWRkIjoiMS4yLjMuNCIsInBvcnQiOiI0NDMiLCJpZCI6ImFiY2QxMjM0LTU2NzgtOTBhYi1jZGVmLTEyMzQ1Njc4OTBhYiIsImFpZCI6IjAiLCJzY3kiOiJhdXRvIiwibmV0Ijoid3MiLCJ0eXBlIjoibm9uZSIsImhvc3QiOiJleGFtcGxlLmNvbSIsInBhdGgiOiIvd3MiLCJ0bHMiOiJ0bHMiLCJzbmkiOiJleGFtcGxlLmNvbSIsImFscG4iOiIiLCJmcCI6IiJ9`

	proxies := ParseContent([]byte(raw), "test")
	if len(proxies) != 1 {
		t.Fatalf("expected 1 proxy, got %d", len(proxies))
	}
	p := proxies[0]
	if p.Type != "vmess" {
		t.Errorf("type = %s", p.Type)
	}
	if p.Network != "ws" {
		t.Errorf("network = %s", p.Network)
	}
	if p.WSPath != "/ws" {
		t.Errorf("ws path = %s", p.WSPath)
	}
}

func TestParseBase64Multi(t *testing.T) {
	// base64 encoded multiple links
	raw := "dmxlc3M6Ly9kNWQ0YjAyMS01Y2FlLTRlNTYtOTQyOS0wNTg2NTVhYjJhZGZAcHgudGhlLWphenplZC5jb206NDQzP3R5cGU9dGNwJmVuY3J5cHRpb249bm9uZSZzZWN1cml0eT1yZWFsaXR5JnBiaz1FVkN1ZnkzOGdYU3N4ZU5RY0ZTZjRoVUJ2b2swSl9RLWQzV2d4bkVleWpJJmZwPWNocm9tZSZzbmk9d3d3Lm52aWRpYS5jb20mc2lkPTk0JnNweD0lMkYmZmxvdz14dGxzLXJwcngtdmlzaW9uI1ZsZXNzJTIwUmVhbGl0eQ=="

	proxies := ParseContent([]byte(raw), "test")
	if len(proxies) != 1 {
		t.Fatalf("expected 1 proxy, got %d", len(proxies))
	}
	if proxies[0].RealityShortID != "94" {
		t.Errorf("short-id = %q", proxies[0].RealityShortID)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsCheck(s, sub))
}

func containsCheck(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
