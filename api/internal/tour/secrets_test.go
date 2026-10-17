package tour

import "testing"

func TestExcluded(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{".env", true},
		{".env.local", true},
		{".env.example", false},
		{".env.sample", false},
		{".env.template", false},
		{"server.pem", true},
		{"id_rsa", true},
		{"id_rsa.pub", true},
		{"id_ed25519", true},
		{".npmrc", true},
		{".netrc", true},
		{"terraform.tfvars", true},
		{"terraform.tfstate", true},
		{"terraform.tfstate.backup", true},
		{".ssh/config", true},
		{".aws/credentials", true},
		{"config/serviceAccount.json", true},
		{"config/SECRET_KEY.txt", true},
		{"internal/store/credentials.go", true},
		{"README.md", false},
		{"src/config.ts", false},
		{"package.json", false},
	}
	for _, tc := range tests {
		if got := Excluded(tc.path); got != tc.want {
			t.Errorf("Excluded(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestReadable(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"README.md", true},
		{"README", true},
		{"CONTRIBUTING.md", true},
		{"CLAUDE.md", true},
		{"AGENTS.md", true},
		{"package.json", true},
		{"go.mod", true},
		{"Makefile", true},
		{"docker-compose.yml", true},
		{"compose.yaml", true},
		{".env.example", true},
		{"api/README.md", true},
		{"api/internal/tour/x.go", false},
		{"src/components/README.md", true},
		{"src/components/sub/README.md", false},
		{"random.txt", false},
		{"api/go.mod", true},
	}
	for _, tc := range tests {
		if got := Readable(tc.path); got != tc.want {
			t.Errorf("Readable(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}
