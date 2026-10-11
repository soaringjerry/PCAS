package comparison

import "testing"

func TestKeeperOfTwoNamesOfOneEntity(t *testing.T) {
	for name, c := range map[string]struct {
		a, b     NamedEntity
		fallback int
		want     int
	}{
		"a topic gives way to a place":       {NamedEntity{Type: "topic", MemoryCount: 9}, NamedEntity{Type: "place", MemoryCount: 1}, 0, 1},
		"an organization is kept over topic": {NamedEntity{Type: "organization"}, NamedEntity{Type: "topic", MemoryCount: 9}, 1, 0},
		"more memories are kept":             {NamedEntity{Type: "person", MemoryCount: 2}, NamedEntity{Type: "person", MemoryCount: 5}, 0, 1},
		"a Chinese name is kept":             {NamedEntity{Type: "person", Name: "Fictitious Wang"}, NamedEntity{Type: "person", Name: "虚构老王"}, 0, 1},
		"the model decides a tie":            {NamedEntity{Type: "person", Name: "虚构甲"}, NamedEntity{Type: "person", Name: "虚构乙"}, 1, 1},
	} {
		if got := KeepIndex(c.a, c.b, c.fallback); got != c.want {
			t.Errorf("%s: got %d", name, got)
		}
	}
}
