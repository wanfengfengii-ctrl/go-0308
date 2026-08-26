package domain

import "testing"

func TestStageOrdering(t *testing.T) {
	want := []Stage{
		StageTopologyLock,
		StageIsolation,
		StageFlush,
		StageDisinfect,
		StageContact,
		StageReflush,
		StageSampling,
		StageReview,
		StageTerminal,
	}
	for i, s := range want {
		if got := s.Order(); got != i {
			t.Fatalf("Order(%q) = %d, want %d", s, got, i)
		}
		if !s.Valid() {
			t.Fatalf("stage %q should be valid", s)
		}
	}
}

func TestStageNext(t *testing.T) {
	cases := []struct {
		in   Stage
		want Stage
	}{
		{StageTopologyLock, StageIsolation},
		{StageIsolation, StageFlush},
		{StageFlush, StageDisinfect},
		{StageDisinfect, StageContact},
		{StageContact, StageReflush},
		{StageReflush, StageSampling},
		{StageSampling, StageReview},
		{StageReview, StageTerminal},
		{StageTerminal, ""},
		{Stage("unknown"), ""},
	}
	for _, c := range cases {
		if got := c.in.Next(); got != c.want {
			t.Fatalf("Next(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestStageUnknownInvalid(t *testing.T) {
	if Stage("bogus").Valid() {
		t.Fatal("unknown stage should be invalid")
	}
	if Stage("bogus").Order() != -1 {
		t.Fatal("unknown stage should have order -1")
	}
}
