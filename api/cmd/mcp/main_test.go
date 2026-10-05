package main

import "testing"

func TestAPIURL(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"default", nil, "http://127.0.0.1:3001"},
		{"API_PORT", map[string]string{"API_PORT": "4001"}, "http://127.0.0.1:4001"},
		{"DEVDIGEST_API_URL wins", map[string]string{"API_PORT": "4001", "DEVDIGEST_API_URL": "http://api.test"}, "http://api.test"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := apiURL(func(k string) string { return tt.env[k] }); got != tt.want {
				t.Errorf("apiURL() = %q, want %q", got, tt.want)
			}
		})
	}
}
