package k8s

import "testing"

func TestByName(t *testing.T) {
	rk, ok := ByName("pods")
	if !ok {
		t.Fatal("expected pods to be found in registry")
	}
	if rk.Kind != "Pod" || rk.Group != "Workloads" || !rk.Namespaced {
		t.Errorf("unexpected ResourceKind for pods: %+v", rk)
	}

	if _, ok := ByName("not-a-real-kind"); ok {
		t.Error("expected unknown kind to not be found")
	}
}

func TestGroups(t *testing.T) {
	groups := Groups()
	if len(groups) == 0 {
		t.Fatal("expected at least one group")
	}
	want := map[string]bool{"Workloads": false, "Network": false, "Config": false, "Storage": false, "Cluster": false}
	for _, g := range groups {
		if _, ok := want[g]; ok {
			want[g] = true
		}
	}
	for g, found := range want {
		if !found {
			t.Errorf("expected group %q to be present in Groups()", g)
		}
	}
}

func TestInGroup(t *testing.T) {
	workloads := InGroup("Workloads")
	if len(workloads) == 0 {
		t.Fatal("expected Workloads group to have entries")
	}
	for _, rk := range workloads {
		if rk.Group != "Workloads" {
			t.Errorf("InGroup(\"Workloads\") returned a kind from group %q", rk.Group)
		}
	}

	if got := InGroup("NoSuchGroup"); got != nil {
		t.Errorf("expected nil for unknown group, got %v", got)
	}
}
