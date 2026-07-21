package knowledge

import "testing"

// TestModelRole covers the mapping from a model ID to the engine role that
// protects it from a prune. An unconfigured engine must not mark every model as
// in use, and it must not mark an empty ID as matching an empty config value —
// that would make an unconfigured model look protected.
func TestModelRole(t *testing.T) {
	tests := []struct {
		name      string
		id        string
		embedding string
		rerank    string
		want      string
	}{
		{"embedding model", "abc", "abc", "xyz", ModelRoleEmbedding},
		{"rerank model", "xyz", "abc", "xyz", ModelRoleRerank},
		{"stray model", "def", "abc", "xyz", ""},
		{"nothing configured", "abc", "", "", ""},
		{"empty id against empty config", "", "", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ModelRole(tt.id, tt.embedding, tt.rerank); got != tt.want {
				t.Errorf("ModelRole(%q, %q, %q) = %q, want %q", tt.id, tt.embedding, tt.rerank, got, tt.want)
			}
		})
	}
}

// TestModelInfoDeployed verifies which states count as occupying memory. A
// partial or failed deployment holds memory without being usable, so it must
// report as deployed — those are exactly the models worth reclaiming.
func TestModelInfoDeployed(t *testing.T) {
	tests := []struct {
		state       string
		workerNodes int
		want        bool
	}{
		{"DEPLOYED", 2, true},
		{"PARTIALLY_DEPLOYED", 1, true},
		{"DEPLOY_FAILED", 0, true},
		{"DEPLOYING", 0, true},
		{"REGISTERED", 0, false},
		{"UNDEPLOYED", 0, false},
		// A state we do not know about still counts when nodes hold it.
		{"SOMETHING_NEW", 3, true},
	}

	for _, tt := range tests {
		t.Run(tt.state, func(t *testing.T) {
			m := ModelInfo{State: tt.state, WorkerNodes: tt.workerNodes}
			if got := m.Deployed(); got != tt.want {
				t.Errorf("ModelInfo{State: %q, WorkerNodes: %d}.Deployed() = %v, want %v",
					tt.state, tt.workerNodes, got, tt.want)
			}
		})
	}
}
