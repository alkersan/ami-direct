package main

import (
	"slices"
	"testing"

	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

func TestParseArchitecture(t *testing.T) {
	got, err := parseArchitecture("x86_64")
	if err != nil {
		t.Fatalf("parseArchitecture returned error: %v", err)
	}
	if got != ec2types.ArchitectureValuesX8664 {
		t.Fatalf("parseArchitecture returned %q, want %q", got, ec2types.ArchitectureValuesX8664)
	}
}

func TestParseArchitectureRejectsInvalidValue(t *testing.T) {
	if _, err := parseArchitecture("not-real"); err == nil {
		t.Fatal("parseArchitecture should fail for invalid architecture")
	}
}

func TestToEC2ImageTags(t *testing.T) {
	got := toEC2ImageTags([]SnapshotTag{
		{Key: "Name", Value: "my-ami"},
		{Key: "env", Value: "prod"},
	})
	if len(got) != 1 {
		t.Fatalf("len(toEC2ImageTags) = %d, want 1", len(got))
	}
	if got[0].ResourceType != ec2types.ResourceTypeImage {
		t.Fatalf("resource type = %q, want %q", got[0].ResourceType, ec2types.ResourceTypeImage)
	}

	wantTags := []ec2types.Tag{
		{Key: stringPtr("Name"), Value: stringPtr("my-ami")},
		{Key: stringPtr("env"), Value: stringPtr("prod")},
	}
	if !slices.EqualFunc(got[0].Tags, wantTags, func(a, b ec2types.Tag) bool {
		return *a.Key == *b.Key && *a.Value == *b.Value
	}) {
		t.Fatalf("tags = %#v, want %#v", got[0].Tags, wantTags)
	}
}

func stringPtr(s string) *string {
	return &s
}
