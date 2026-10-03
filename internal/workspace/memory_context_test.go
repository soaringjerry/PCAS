package workspace

import (
	"encoding/json"
	"testing"
)

func TestOldMemoryGetsContextReadingHintWithoutChangingText(t *testing.T) {
	for _, test := range []struct {
		text  string
		needs bool
	}{{"那位同事的演示项目值得联系。", true}, {"我认为其他方案需要继续讨论。", false}, {"演示项目的联系人是测试人物甲。", false}} {
		original := Memory{Text: test.text, Version: 1}
		raw, err := json.Marshal(original)
		if err != nil {
			t.Fatal(err)
		}
		var decoded Memory
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded.ContextDependent != test.needs || decoded.Text != original.Text || decoded.Version != original.Version {
			t.Fatal("reading hint modified or misclassified memory")
		}
	}
}
