package knowledge

import "testing"

// TestResolveKapaClient pins the one rule both the CLI and the daemon use to
// decide whether a kapa client exists.
func TestResolveKapaClient(t *testing.T) {
	tests := []struct {
		name      string
		enabled   string // raw kapa.enabled value
		projectID string
		apiKey    string
		want      bool
	}{
		{name: "enabled with both values", enabled: "true", projectID: "proj", apiKey: "key", want: true},
		{name: "unset enabled defaults to true", enabled: "", projectID: "proj", apiKey: "key", want: true},
		{name: "missing key", enabled: "true", projectID: "proj", apiKey: "", want: false},
		{name: "missing project", enabled: "true", projectID: "", apiKey: "key", want: false},
		{name: "explicitly disabled", enabled: "false", projectID: "proj", apiKey: "key", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := ResolveKapaClient(KapaEnabled(tt.enabled), tt.projectID, tt.apiKey)
			if got := client != nil; got != tt.want {
				t.Fatalf("client built = %v, want %v", got, tt.want)
			}
			if client != nil && (client.projectID != tt.projectID || client.apiKey != tt.apiKey) {
				t.Errorf("client = {project %q, key %q}, want {%q, %q}",
					client.projectID, client.apiKey, tt.projectID, tt.apiKey)
			}
		})
	}
}

func TestKapaEnabled(t *testing.T) {
	for raw, want := range map[string]bool{"": true, "true": true, "1": true, "false": false, "0": false} {
		if got := KapaEnabled(raw); got != want {
			t.Errorf("KapaEnabled(%q) = %v, want %v", raw, got, want)
		}
	}
}
