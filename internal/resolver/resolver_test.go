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

// ─── ByContinent ──────────────────────────────────────────────────────────────

func TestByContinent_EmptyInput(t *testing.T) {
	t.Parallel()

	got := ByContinent(nil)
	if len(got) != 0 {
		t.Errorf("ByContinent(nil) returned %d groups; want 0", len(got))
	}

	got = ByContinent([]Resolver{})
	if len(got) != 0 {
		t.Errorf("ByContinent([]) returned %d groups; want 0", len(got))
	}
}

func TestByContinent_EmptyContinentFallsBackToUnknown(t *testing.T) {
	t.Parallel()

	input := []Resolver{
		{IP: "1.2.3.4", Name: "Test", Country: "Nowhere", City: "Void", Continent: ""},
	}

	got := ByContinent(input)

	unknown, ok := got["Unknown"]
	if !ok {
		t.Fatal("ByContinent() did not create an 'Unknown' key for resolver with empty Continent")
	}
	if len(unknown) != 1 {
		t.Errorf("'Unknown' group has %d entries; want 1", len(unknown))
	}
	if unknown[0] != input[0] {
		t.Errorf("'Unknown' group[0] = %+v; want %+v", unknown[0], input[0])
	}
}

func TestByContinent_GroupsCorrectly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     []Resolver
		wantKeys  []string
		wantSizes map[string]int
	}{
		{
			name: "single continent",
			input: []Resolver{
				{IP: "1.1.1.1", Name: "A", Country: "X", City: "Y", Continent: "Europe"},
				{IP: "2.2.2.2", Name: "B", Country: "X", City: "Y", Continent: "Europe"},
			},
			wantKeys:  []string{"Europe"},
			wantSizes: map[string]int{"Europe": 2},
		},
		{
			name: "two continents",
			input: []Resolver{
				{IP: "1.1.1.1", Name: "A", Country: "X", City: "Y", Continent: "Europe"},
				{IP: "2.2.2.2", Name: "B", Country: "X", City: "Y", Continent: "Asia"},
				{IP: "3.3.3.3", Name: "C", Country: "X", City: "Y", Continent: "Asia"},
			},
			wantKeys:  []string{"Europe", "Asia"},
			wantSizes: map[string]int{"Europe": 1, "Asia": 2},
		},
		{
			name: "mixed with unknown",
			input: []Resolver{
				{IP: "1.1.1.1", Name: "A", Country: "X", City: "Y", Continent: "Africa"},
				{IP: "2.2.2.2", Name: "B", Country: "X", City: "Y", Continent: ""},
				{IP: "3.3.3.3", Name: "C", Country: "X", City: "Y", Continent: ""},
			},
			wantKeys:  []string{"Africa", "Unknown"},
			wantSizes: map[string]int{"Africa": 1, "Unknown": 2},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := ByContinent(tc.input)

			if len(got) != len(tc.wantSizes) {
				t.Errorf("ByContinent() returned %d groups; want %d", len(got), len(tc.wantSizes))
			}
			for _, key := range tc.wantKeys {
				group, ok := got[key]
				if !ok {
					t.Errorf("ByContinent() missing expected key %q", key)
					continue
				}
				if len(group) != tc.wantSizes[key] {
					t.Errorf("group[%q] has %d entries; want %d", key, len(group), tc.wantSizes[key])
				}
			}
		})
	}
}

func TestByContinent_TotalCountMatchesInput(t *testing.T) {
	t.Parallel()

	resolvers := All()
	groups := ByContinent(resolvers)

	total := 0
	for _, group := range groups {
		total += len(group)
	}

	if total != len(resolvers) {
		t.Errorf("sum of all ByContinent groups = %d; want %d (input length)", total, len(resolvers))
	}
}

func TestByContinent_TotalCountMatchesInput_Synthetic(t *testing.T) {
	t.Parallel()

	input := []Resolver{
		{IP: "1.1.1.1", Name: "A", Country: "X", City: "Y", Continent: "Europe"},
		{IP: "2.2.2.2", Name: "B", Country: "X", City: "Y", Continent: "Asia"},
		{IP: "3.3.3.3", Name: "C", Country: "X", City: "Y", Continent: ""},
		{IP: "4.4.4.4", Name: "D", Country: "X", City: "Y", Continent: "Europe"},
		{IP: "5.5.5.5", Name: "E", Country: "X", City: "Y", Continent: "Africa"},
	}

	groups := ByContinent(input)

	total := 0
	for _, group := range groups {
		total += len(group)
	}
	if total != len(input) {
		t.Errorf("sum of ByContinent groups = %d; want %d", total, len(input))
	}
}

func TestByContinent_PreservesResolverData(t *testing.T) {
	t.Parallel()

	r := Resolver{IP: "9.9.9.9", Name: "Quad9 Primary", Country: "Global", City: "Anycast", Continent: "Global"}
	got := ByContinent([]Resolver{r})

	group, ok := got["Global"]
	if !ok {
		t.Fatal("ByContinent() did not create a 'Global' key")
	}
	if len(group) != 1 {
		t.Fatalf("'Global' group has %d entries; want 1", len(group))
	}
	if group[0] != r {
		t.Errorf("resolver in group = %+v; want %+v", group[0], r)
	}
}

func TestByContinent_SingleResolverPerContinent(t *testing.T) {
	t.Parallel()

	input := []Resolver{
		{IP: "1.1.1.1", Name: "A", Country: "X", City: "Y", Continent: "Europe"},
		{IP: "2.2.2.2", Name: "B", Country: "X", City: "Y", Continent: "Asia"},
		{IP: "3.3.3.3", Name: "C", Country: "X", City: "Y", Continent: "Oceania"},
	}

	got := ByContinent(input)

	if len(got) != 3 {
		t.Errorf("ByContinent() returned %d groups; want 3", len(got))
	}
	for key, group := range got {
		if len(group) != 1 {
			t.Errorf("group[%q] has %d entries; want 1", key, len(group))
		}
	}
}

func TestByContinent_OnFullList_ContainsContinents(t *testing.T) {
	t.Parallel()

	// The curated list must include resolvers for these continents at minimum.
	wantContinents := []string{
		"Global",
		"North America",
		"Europe",
		"Asia",
		"Oceania",
		"South America",
		"Africa",
		"Middle East",
	}

	groups := ByContinent(All())

	for _, continent := range wantContinents {
		t.Run(continent, func(t *testing.T) {
			t.Parallel()

			group, ok := groups[continent]
			if !ok {
				t.Errorf("ByContinent(All()) missing continent %q", continent)
				return
			}
			if len(group) == 0 {
				t.Errorf("continent %q has 0 resolvers; expected at least 1", continent)
			}
		})
	}
}

func TestByContinent_OnFullList_NoUnknownGroup(t *testing.T) {
	t.Parallel()

	// Every resolver in the curated list must have a non-empty Continent so
	// no "Unknown" bucket should exist.
	groups := ByContinent(All())

	if _, ok := groups["Unknown"]; ok {
		t.Errorf("ByContinent(All()) contains an 'Unknown' group; all curated resolvers must have a Continent set")
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

// ─── edge cases ───────────────────────────────────────────────────────────────

func TestByContinent_AllSameContinent(t *testing.T) {
	t.Parallel()

	input := make([]Resolver, 10)
	for i := range input {
		input[i] = Resolver{
			IP:        fmt.Sprintf("10.0.0.%d", i),
			Name:      fmt.Sprintf("Resolver %d", i),
			Country:   "Testland",
			City:      "Testcity",
			Continent: "TestContinent",
		}
	}

	got := ByContinent(input)

	if len(got) != 1 {
		t.Errorf("ByContinent() returned %d groups for single-continent input; want 1", len(got))
	}
	group := got["TestContinent"]
	if len(group) != len(input) {
		t.Errorf("group[TestContinent] has %d entries; want %d", len(group), len(input))
	}
}

func TestByContinent_AllEmptyContinent(t *testing.T) {
	t.Parallel()

	input := []Resolver{
		{IP: "1.1.1.1", Name: "A", Country: "X", City: "Y", Continent: ""},
		{IP: "2.2.2.2", Name: "B", Country: "X", City: "Y", Continent: ""},
	}

	got := ByContinent(input)

	if len(got) != 1 {
		t.Errorf("ByContinent() returned %d groups; want 1 ('Unknown')", len(got))
	}
	unknown, ok := got["Unknown"]
	if !ok {
		t.Fatal("ByContinent() missing 'Unknown' key when all continents are empty")
	}
	if len(unknown) != 2 {
		t.Errorf("'Unknown' group has %d entries; want 2", len(unknown))
	}
}

func TestByContinent_SingleResolver_NamedContinent(t *testing.T) {
	t.Parallel()

	r := Resolver{IP: "1.2.3.4", Name: "Test", Country: "TestLand", City: "TestCity", Continent: "Mars"}
	got := ByContinent([]Resolver{r})

	group, ok := got["Mars"]
	if !ok {
		t.Fatal("ByContinent() did not create group for continent 'Mars'")
	}
	if len(group) != 1 {
		t.Fatalf("group[Mars] has %d entries; want 1", len(group))
	}
	if group[0] != r {
		t.Errorf("group[Mars][0] = %+v; want %+v", group[0], r)
	}
}

func TestByContinent_DoesNotMutateInput(t *testing.T) {
	t.Parallel()

	input := []Resolver{
		{IP: "1.1.1.1", Name: "A", Country: "X", City: "Y", Continent: "Europe"},
		{IP: "2.2.2.2", Name: "B", Country: "X", City: "Y", Continent: "Asia"},
	}
	original := make([]Resolver, len(input))
	copy(original, input)

	ByContinent(input)

	for i := range input {
		if input[i] != original[i] {
			t.Errorf("ByContinent() mutated input[%d]: got %+v; want %+v", i, input[i], original[i])
		}
	}
}
