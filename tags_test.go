package main

import (
	"slices"
	"testing"
)

func TestParseSnapshotTags(t *testing.T) {
	got, err := parseTags([]string{"Name=disk-image", "env=prod", "empty="})
	if err != nil {
		t.Fatalf("parseTags returned error: %v", err)
	}

	want := []SnapshotTag{
		{Key: "Name", Value: "disk-image"},
		{Key: "env", Value: "prod"},
		{Key: "empty", Value: ""},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("parseTags = %#v, want %#v", got, want)
	}
}

func TestParseSnapshotTagsRejectsInvalidInput(t *testing.T) {
	tests := []string{
		"missing-separator",
		"=value",
	}

	for _, rawTag := range tests {
		if _, err := parseTags([]string{rawTag}); err == nil {
			t.Fatalf("parseTags(%q) should fail", rawTag)
		}
	}
}

func TestWithDefaultNameTag(t *testing.T) {
	got := withDefaultNameTag([]SnapshotTag{{Key: "env", Value: "prod"}}, "my-ami")
	want := []SnapshotTag{
		{Key: "env", Value: "prod"},
		{Key: "Name", Value: "my-ami"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("withDefaultNameTag = %#v, want %#v", got, want)
	}
}

func TestWithDefaultNameTagPreservesExplicitNameTag(t *testing.T) {
	input := []SnapshotTag{
		{Key: "Name", Value: "explicit"},
		{Key: "env", Value: "prod"},
	}
	got := withDefaultNameTag(input, "derived")
	if !slices.Equal(got, input) {
		t.Fatalf("withDefaultNameTag = %#v, want %#v", got, input)
	}
}
