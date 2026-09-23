package main

import "testing"

func TestNewWindowRequested(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "normal launch", args: nil, want: false},
		{name: "explicit window", args: []string{"--new-window"}, want: true},
		{name: "other arguments", args: []string{"--profile", "work"}, want: false},
		{name: "does not match a prefix", args: []string{"--new-window=true"}, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := newWindowRequested(test.args); got != test.want {
				t.Fatalf("newWindowRequested(%v) = %v, want %v", test.args, got, test.want)
			}
		})
	}
}
