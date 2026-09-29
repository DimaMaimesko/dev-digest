package main

import (
	"log/slog"
	"strings"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		want    config
		wantErr string
	}{
		{
			name: "defaults match the TS server",
			want: config{
				databaseURL: "postgres://devdigest:devdigest@localhost:5433/devdigest",
				port:        3001,
				webOrigin:   "http://localhost:3000",
				logLevel:    slog.LevelInfo,
			},
		},
		{
			name: "everything set",
			env:  map[string]string{"DATABASE_URL": "postgres://x/y", "API_PORT": "3002", "WEB_PORT": "4000", "LOG_LEVEL": "debug"},
			want: config{databaseURL: "postgres://x/y", port: 3002, webOrigin: "http://localhost:4000", logLevel: slog.LevelDebug},
		},
		{
			// .env.example ships LOG_LEVEL= empty.
			name: "empty LOG_LEVEL means info",
			env:  map[string]string{"LOG_LEVEL": ""},
			want: config{databaseURL: "postgres://devdigest:devdigest@localhost:5433/devdigest", port: 3001, webOrigin: "http://localhost:3000", logLevel: slog.LevelInfo},
		},
		{name: "bad port", env: map[string]string{"API_PORT": "abc"}, wantErr: `API_PORT "abc"`},
		{name: "bad web port", env: map[string]string{"WEB_PORT": "x"}, wantErr: `WEB_PORT "x"`},
		{name: "bad log level", env: map[string]string{"LOG_LEVEL": "loud"}, wantErr: `LOG_LEVEL "loud"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := loadConfig(func(k string) string { return tt.env[k] })
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("loadConfig: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

func TestSilentLogLevel(t *testing.T) {
	cfg, err := loadConfig(func(k string) string { return map[string]string{"LOG_LEVEL": "silent"}[k] })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.logLevel <= slog.LevelError {
		t.Errorf("silent level %v would still log errors", cfg.logLevel)
	}
}
