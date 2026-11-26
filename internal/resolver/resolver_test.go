package resolver

import (
	"fmt"
	"testing"
)

// ─── initResolvers ────────────────────────────────────────────────────────────

func TestInitResolvers_ReturnsNonEmpty(t *testing.T) {
	t.Parallel()

	got := initResolvers()
	if len(got) == 0 {
		t.Fatal("initResolvers() returned an empty slice; expected at least one resolver")
	}
}

func TestInitResolvers_AllFieldsNonEmpty(t *testing.T) {
	t.Parallel()

	resolvers := initResolvers()
	for i, r := range resolvers {
		t.Run(fmt.Sprintf("index_%d_%s", i, r.IP), func(t *testing.T) {
			t.Parallel()

			if r.IP == "" {
				t.Errorf("resolver[%d] has empty IP", i)
			}
			if r.Name == "" {
				t.Errorf("resolver[%d] (IP=%s) has empty Name", i, r.IP)
			}
			if r.Country == "" {
				t.Errorf("resolver[%d] (IP=%s) has empty Country", i, r.IP)
			}
			if r.Continent == "" {
				t.Errorf("resolver[%d] (IP=%s) has empty Continent", i, r.IP)
			}
		})
	}
}

func TestInitResolvers_NoDuplicateIPs(t *testing.T) {
	t.Parallel()

	resolvers := initResolvers()
	seen := make(map[string]int, len(resolvers))
	for i, r := range resolvers {
		if prev, exists := seen[r.IP]; exists {
			t.Errorf("duplicate IP %q at index %d (first seen at index %d)", r.IP, i, prev)
		}
		seen[r.IP] = i
	}
}

func TestInitResolvers_IsDeterministic(t *testing.T) {
	t.Parallel()

	first := initResolvers()
	second := initResolvers()

	if len(first) != len(second) {
		t.Fatalf("initResolvers() returned different lengths across calls: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Errorf("resolver at index %d differs between calls: %+v vs %+v", i, first[i], second[i])
		}
	}
}

// ─── All ──────────────────────────────────────────────────────────────────────

func TestAll_ReturnsNonEmpty(t *testing.T) {
	t.Parallel()

	got := All()
	if len(got) == 0 {
		t.Fatal("All() returned an empty slice; expected at least one resolver")
	}
}

func TestAll_ReturnsCopy_MutationDoesNotAffectSource(t *testing.T) {
	t.Parallel()

	first := All()
	originalLen := len(first)

	// Overwrite every element in the returned slice.
	for i := range first {
		first[i] = Resolver{IP: "0.0.0.0", Name: "mutated", Country: "x", City: "x", Continent: "x"}
	}

	second := All()
	if len(second) != originalLen {
		t.Fatalf("All() returned %d entries after mutation; want %d", len(second), originalLen)
	}
	for i, r := range second {
		if r.IP == "0.0.0.0" {
			t.Errorf("All() returned mutated resolver at index %d (%+v); package-level list was corrupted", i, r)
		}
	}
}

func TestAll_ReturnsCopy_AppendDoesNotAffectSource(t *testing.T) {
	t.Parallel()

	first := All()
	lenBefore := len(first)

	// Append to the returned slice (tests that the backing array is not shared).
	_ = append(first, Resolver{IP: "1.2.3.4", Name: "extra", Country: "x", City: "x", Continent: "x"})

	second := All()
	if len(second) != lenBefore {
		t.Errorf("All() length changed after append to prior return; got %d, want %d", len(second), lenBefore)
	}
}

func TestAll_ConsistentAcrossCalls(t *testing.T) {
	t.Parallel()

	a := All()
	b := All()
	c := All()

	if len(a) != len(b) || len(b) != len(c) {
		t.Fatalf("All() returned inconsistent lengths: %d, %d, %d", len(a), len(b), len(c))
	}
	for i := range a {
		if a[i] != b[i] || b[i] != c[i] {
			t.Errorf("resolver at index %d differs across calls: %+v / %+v / %+v", i, a[i], b[i], c[i])
		}
	}
}

func TestAll_AllFieldsNonEmpty(t *testing.T) {
	t.Parallel()

	resolvers := All()
	for i, r := range resolvers {
		t.Run(fmt.Sprintf("index_%d_%s", i, r.IP), func(t *testing.T) {
			t.Parallel()

			if r.IP == "" {
				t.Errorf("All()[%d] has empty IP", i)
			}
			if r.Name == "" {
				t.Errorf("All()[%d] (IP=%s) has empty Name", i, r.IP)
			}
			if r.Country == "" {
				t.Errorf("All()[%d] (IP=%s) has empty Country", i, r.IP)
			}
			if r.Continent == "" {
				t.Errorf("All()[%d] (IP=%s) has empty Continent", i, r.IP)
			}
		})
	}
}

func TestAll_NoDuplicateIPs(t *testing.T) {
	t.Parallel()

	resolvers := All()
	seen := make(map[string]int, len(resolvers))
	for i, r := range resolvers {
		if prev, exists := seen[r.IP]; exists {
			t.Errorf("All() contains duplicate IP %q at index %d (first seen at index %d)", r.IP, i, prev)
		}
		seen[r.IP] = i
	}
}

func TestAll_MatchesInitResolvers(t *testing.T) {
	t.Parallel()

	fromAll := All()
	fromInit := initResolvers()

	if len(fromAll) != len(fromInit) {
		t.Fatalf("All() returned %d entries; initResolvers() returned %d", len(fromAll), len(fromInit))
	}
	for i := range fromAll {
		if fromAll[i] != fromInit[i] {
			t.Errorf("All()[%d] = %+v; initResolvers()[%d] = %+v", i, fromAll[i], i, fromInit[i])
		}
	}
}

// ─── Resolver struct ──────────────────────────────────────────────────────────

func TestResolverStruct_ZeroValue(t *testing.T) {
	t.Parallel()

	var r Resolver
	if r.IP != "" || r.Name != "" || r.Country != "" || r.City != "" || r.Continent != "" {
		t.Errorf("zero-value Resolver fields are not all empty: %+v", r)
	}
}

func TestResolverStruct_FieldAssignment(t *testing.T) {
	t.Parallel()

	r := Resolver{
		IP:        "8.8.8.8",
		Name:      "Google Public DNS Primary",
		Country:   "Global",
		City:      "Anycast",
		Continent: "Global",
	}

	if r.IP != "8.8.8.8" {
		t.Errorf("IP = %q; want %q", r.IP, "8.8.8.8")
	}
	if r.Name != "Google Public DNS Primary" {
		t.Errorf("Name = %q; want %q", r.Name, "Google Public DNS Primary")
	}
	if r.Country != "Global" {
		t.Errorf("Country = %q; want %q", r.Country, "Global")
	}
	if r.City != "Anycast" {
		t.Errorf("City = %q; want %q", r.City, "Anycast")
	}
	if r.Continent != "Global" {
		t.Errorf("Continent = %q; want %q", r.Continent, "Global")
	}
}

// ─── package-level var ────────────────────────────────────────────────────────

func TestPackageVar_all_MatchesInitResolvers(t *testing.T) {
	t.Parallel()

	// The package-level `all` variable (initialised once at startup) must
	// be identical in content to a fresh call to initResolvers().
	fresh := initResolvers()

	if len(all) != len(fresh) {
		t.Fatalf("package-level all has %d entries; initResolvers() returns %d", len(all), len(fresh))
	}
	for i := range all {
		if all[i] != fresh[i] {
			t.Errorf("all[%d] = %+v; initResolvers()[%d] = %+v", i, all[i], i, fresh[i])
		}
	}
}
