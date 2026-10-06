package main

import (
	"reflect"
	"testing"
)

func TestCommandArgs(t *testing.T) {
	cases := []struct {
		name string
		args []string
		app  bool
		want []string
	}{
		{"cli no args", nil, false, nil},
		{"app no args serves", nil, true, []string{"serve"}},
		{"app finder psn serves", []string{"-psn_0_12345"}, true, []string{"serve"}},
		{"app explicit command", []string{"status"}, true, []string{"status"}},
		{"cli command", []string{"login", "codex"}, false, []string{"login", "codex"}},
	}
	for _, c := range cases {
		got := commandArgs(c.args, c.app)
		if len(got) == 0 && len(c.want) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: commandArgs(%v, %v) = %v, want %v", c.name, c.args, c.app, got, c.want)
		}
	}
}
