package processing

import "testing"

func TestRepoSourceID(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		path      string
		want      string
	}{
		{"github root file", "canonical/k8s-snap", "README.md", "canonical/k8s-snap/README.md"},
		{"github nested file", "canonical/k8s-snap", "docs/src/index.md", "canonical/k8s-snap/docs/src/index.md"},
		{"gitea namespace", "opendev.org/openstack/nova", "README.rst", "opendev.org/openstack/nova/README.rst"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RepoSourceID(tt.namespace, tt.path); got != tt.want {
				t.Errorf("RepoSourceID(%q, %q) = %q, want %q", tt.namespace, tt.path, got, tt.want)
			}
		})
	}
}

// The same filename in different repos must not collapse onto one source id:
// source metadata is keyed globally, so a collision suppresses the second
// ingest (or overwrites the first one's metadata under --force).
func TestRepoSourceIDDistinguishesRepos(t *testing.T) {
	a := RepoSourceID("canonical/k8s-snap", "README.md")
	b := RepoSourceID("canonical/microk8s", "README.md")
	if a == b {
		t.Fatalf("source ids for the same filename in different repos collide: %q", a)
	}
}

func TestGiteaRepoNamespace(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		owner   string
		repo    string
		want    string
	}{
		{"https base", "https://opendev.org", "openstack", "nova", "opendev.org/openstack/nova"},
		{"base with port", "https://git.example.com:3000", "team", "docs", "git.example.com:3000/team/docs"},
		{"unparseable base falls back", "not a url", "team", "docs", "not a url/team/docs"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GiteaRepoNamespace(tt.baseURL, tt.owner, tt.repo); got != tt.want {
				t.Errorf("GiteaRepoNamespace(%q, %q, %q) = %q, want %q",
					tt.baseURL, tt.owner, tt.repo, got, tt.want)
			}
		})
	}
}

// Different Gitea instances can host the same owner/repo pair; the host keeps
// their source ids apart.
func TestGiteaRepoNamespaceDistinguishesHosts(t *testing.T) {
	a := GiteaRepoNamespace("https://opendev.org", "openstack", "nova")
	b := GiteaRepoNamespace("https://git.internal", "openstack", "nova")
	if a == b {
		t.Fatalf("namespaces for the same owner/repo on different hosts collide: %q", a)
	}
}
