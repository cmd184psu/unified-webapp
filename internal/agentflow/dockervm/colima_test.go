package dockervm

import (
	"reflect"
	"testing"
)

func TestColimaMountsReadsTheSharedDirectories(t *testing.T) {
	yaml := `cpu: 6
# Colima's mounts
mounts:
  - location: /opt/unified-webapp/data/agentflow/worktrees
    writable: true
  - location: ~/Games/Civ3Complete
    writable: false
  - location: "/Users/me/work/re"
    writable: false
mountType: virtiofs
`
	got := colimaMounts(yaml, "/Users/me")
	want := []string{"/opt/unified-webapp/data/agentflow/worktrees", "/Users/me/Games/Civ3Complete", "/Users/me/work/re"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mounts = %v, want %v", got, want)
	}
	if got := colimaMounts("cpu: 2\nmounts: []\n", "/Users/me"); !reflect.DeepEqual(got, []string{"/Users/me", "/tmp/colima"}) {
		t.Fatalf("no mounts listed is Colima's default share, got %v", got)
	}
}
