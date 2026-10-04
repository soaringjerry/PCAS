package main

import (
	"reflect"
	"testing"
)

func TestFreezeDispatchDoesNotConsumeExistingMode(t *testing.T) {
	for _, args := range [][]string{{"-mode=doing"}, {"-mode=doing-propose"}, {"-mode=lookup"}, {"-mode", "doing"}} {
		if _, ok := doingFreezeArgs(args); ok {
			t.Fatal("existing mode consumed")
		}
	}
	for _, args := range [][]string{{"-suite", "fixture", "-mode=doing-freeze"}, {"-mode", "doing-freeze", "-suite", "fixture"}} {
		got, ok := doingFreezeArgs(args)
		if !ok || !reflect.DeepEqual(got, []string{"-suite", "fixture"}) {
			t.Fatalf("bad dispatch: %v", got)
		}
	}
}
