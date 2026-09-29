package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/DimaMaimesko/dev-digest/api/internal/pgtest"
)

func TestRun(t *testing.T) {
	db := pgtest.NewEmpty(t)
	env := map[string]string{"DATABASE_URL": db.Config().ConnString()}
	getenv := func(k string) string { return env[k] }
	var out bytes.Buffer
	for _, step := range []struct {
		args []string
		want string
	}{
		{[]string{"migrate"}, "applied 0000_init\n"},
		{[]string{"migrate"}, "the database is up to date\n"},
		{[]string{"seed"}, "seeded: workspace "},
		{[]string{"seed"}, "seeded: workspace "},
	} {
		out.Reset()
		if err := run(context.Background(), step.args, getenv, &out); err != nil {
			t.Fatalf("%v: %v", step.args, err)
		}
		if !strings.Contains(out.String(), step.want) {
			t.Errorf("%v: %q", step.args, out.String())
		}
	}
}

func TestRunUsage(t *testing.T) {
	for _, args := range [][]string{nil, {"drop"}, {"migrate", "-force"}, {"migrate", "-dir", "x"}} {
		err := run(context.Background(), args, func(string) string { return "" }, &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "usage:") {
			t.Errorf("%v: %v", args, err)
		}
	}
}
