package main

import (
	"strings"
	"testing"
)

func TestHelpListsEveryCommand(t *testing.T) {
	var out strings.Builder
	if err := dispatch([]string{"help"}, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	for _, c := range commands() {
		if !strings.Contains(out.String(), c.name) {
			t.Errorf("help does not list %q", c.name)
		}
	}
	for _, c := range commands() {
		out.Reset()
		if err := dispatch([]string{"help", c.name}, strings.NewReader(""), &out); err != nil {
			t.Errorf("help %s: %v", c.name, err)
		}
		if !strings.Contains(out.String(), c.long) {
			t.Errorf("help %s does not print its description", c.name)
		}
	}
}

func TestUnknownCommand(t *testing.T) {
	var out strings.Builder
	if err := dispatch([]string{"bogus"}, strings.NewReader(""), &out); err == nil {
		t.Fatal("expected an error")
	}
	if err := dispatch([]string{"help", "bogus"}, strings.NewReader(""), &out); err == nil || !strings.Contains(err.Error(), "configure") {
		t.Errorf("help bogus should list commands, got %v", err)
	}
}

func TestOpenAPICommand(t *testing.T) {
	var out strings.Builder
	if err := dispatch([]string{"openapi"}, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"openapi": "3.0.3"`) || !strings.Contains(out.String(), `"/health"`) {
		t.Errorf("unexpected document:\n%s", out.String())
	}
}
