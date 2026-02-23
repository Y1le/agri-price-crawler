package stringutil

import "testing"

func TestDiff(t *testing.T) {
	testCase := [][]string{
		{"foo", "bar", "hello"},
		{"foo", "bar", "world"},
	}
	result := Diff(testCase[0], testCase[1])
	if len(result) != 1 || result[0] != "hello" {
		t.Fatalf("Diff failed")
	}
}

func TestUnique(t *testing.T) {
	testCase := []string{"foo", "bar", "hello", "world", "hello"}
	result := Unique(testCase)
	if len(result) != 4 {
		t.Fatalf("Unique failed")
	}
}

func TestStringIn(t *testing.T) {
	testCase := []string{"foo", "bar", "hello", "world"}
	if !StringIn("hello", testCase) {
		t.Fatalf("StringIn failed: shell be string is in array")
	}

	if StringIn("fooo", testCase) {
		t.Fatalf("StringIn failed: shell be string is not in array")
	}
}
