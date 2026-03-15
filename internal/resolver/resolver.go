// Package resolver provides a curated list of public DNS resolvers with
// geographic metadata and utility functions for grouping and filtering them.
package resolver

import (
	"log/slog"
)

// Resolver represents a single public DNS resolver with its network address
// and geographic metadata.
type Resolver struct {
	IP        string
	Name      string
	Country   string
	City      string
	Continent string
}

// all is the internal curated list initialised once at package load time.
// IPs have been verified against publicly documented resolver addresses.
var all = initResolvers()

// initResolvers builds the canonical resolver list and emits structured log
// output so that callers can observe the resolver set at startup.
func initResolvers() []Resolver {
	slog.Debug("resolver: initialising curated public DNS resolver list")

	resolvers := []Resolver{
		// ----------------------------------------------------------------
		// Global / Anycast
		// ----------------------------------------------------------------
		{
			IP:        "8.8.8.8",
			Name:      "Google Public DNS Primary",
			Country:   "Global",
			City:      "Anycast",
			Continent: "Global",
		},
		{
			IP:        "8.8.4.4",
			Name:      "Google Public DNS Secondary",
			Country:   "Global",
			City:      "Anycast",
			Continent: "Global",
		},
		{
			IP:        "1.1.1.1",
			Name:      "Cloudflare Primary",
			Country:   "Global",
			City:      "Anycast",
			Continent: "Global",
		},
		{
			IP:        "1.0.0.1",
			Name:      "Cloudflare Secondary",
			Country:   "Global",
			City:      "Anycast",
			Continent: "Global",
		},
		{
			IP:        "9.9.9.9",
			Name:      "Quad9 Primary",
			Country:   "Global",
			City:      "Anycast",
			Continent: "Global",
		},
		{
			IP:        "149.112.112.112",
			Name:      "Quad9 Secondary",
			Country:   "Global",
			City:      "Anycast",
			Continent: "Global",
		},

		// ----------------------------------------------------------------
		// North America
		// ----------------------------------------------------------------
		{
			IP:        "208.67.222.222",
			Name:      "OpenDNS Primary",
			Country:   "United States",
			City:      "San Francisco",
			Continent: "North America",
		},
		{
			IP:        "208.67.220.220",
			Name:      "OpenDNS Secondary",
			Country:   "United States",
			City:      "San Francisco",
			Continent: "North America",
		},
		{
			IP:        "4.2.2.1",
			Name:      "Level3 Primary",
			Country:   "United States",
			City:      "Broomfield",
			Continent: "North America",
		},
		{
			IP:        "4.2.2.2",
			Name:      "Level3 Secondary",
			Country:   "United States",
			City:      "Broomfield",
			Continent: "North America",
		},
		{
			IP:        "8.26.56.26",
			Name:      "Comodo Secure DNS Primary",
			Country:   "United States",
			City:      "Clifton",
			Continent: "North America",
		},
		{
			IP:        "199.85.126.10",
			Name:      "Norton ConnectSafe Primary",
			Country:   "United States",
			City:      "Culver City",
			Continent: "North America",
		},
		{
			IP:        "64.6.64.6",
			Name:      "Neustar UltraDNS Primary",
			Country:   "United States",
			City:      "Sterling",
			Continent: "North America",
		},
		{
			IP:        "64.6.65.6",
			Name:      "Neustar UltraDNS Secondary",
			Country:   "United States",
			City:      "Sterling",
			Continent: "North America",
		},
		{
			IP:        "185.228.168.168",
			Name:      "CleanBrowsing Primary",
			Country:   "United States",
			City:      "Anycast",
			Continent: "North America",
		},
		{
			IP:        "209.18.47.61",
			Name:      "Spectrum/Charter Primary",
			Country:   "United States",
			City:      "Stamford",
			Continent: "North America",
		},
		{
			IP:        "149.112.121.10",
			Name:      "CIRA Canadian Shield Primary",
			Country:   "Canada",
			City:      "Ottawa",
			Continent: "North America",
		},
		{
			IP:        "149.112.122.10",
			Name:      "CIRA Canadian Shield Secondary",
			Country:   "Canada",
			City:      "Toronto",
			Continent: "North America",
		},

		// ----------------------------------------------------------------
		// Europe
		// ----------------------------------------------------------------
		{
			IP:        "84.200.69.80",
			Name:      "DNS.WATCH Primary",
			Country:   "Germany",
			City:      "Düsseldorf",
			Continent: "Europe",
		},
		{
			IP:        "84.200.70.40",
			Name:      "DNS.WATCH Secondary",
			Country:   "Germany",
			City:      "Düsseldorf",
			Continent: "Europe",
		},
		{
			IP:        "5.1.66.255",
			Name:      "Freifunk Munich",
			Country:   "Germany",
			City:      "Munich",
			Continent: "Europe",
		},
		{
			IP:        "80.67.169.12",
			Name:      "FDN",
			Country:   "France",
			City:      "Paris",
			Continent: "Europe",
		},
		{
			IP:        "91.239.100.100",
			Name:      "UncensoredDNS Primary",
			Country:   "Denmark",
			City:      "Copenhagen",
			Continent: "Europe",
		},
		{
			IP:        "89.233.43.71",
			Name:      "UncensoredDNS Secondary",
			Country:   "Denmark",
			City:      "Copenhagen",
			Continent: "Europe",
		},
		{
			IP:        "185.222.222.222",
			Name:      "DNS.SB Primary",
			Country:   "Estonia",
			City:      "Tallinn",
			Continent: "Europe",
		},
		{
			IP:        "116.202.176.26",
			Name:      "LibreDNS",
			Country:   "Germany",
			City:      "Nuremberg",
			Continent: "Europe",
		},
		{
			IP:        "194.242.2.2",
			Name:      "Mullvad Primary",
			Country:   "Sweden",
			City:      "Stockholm",
			Continent: "Europe",
		},
		{
			IP:        "176.9.93.198",
			Name:      "dnsforge.de Primary",
			Country:   "Germany",
			City:      "Falkenstein",
			Continent: "Europe",
		},
		{
			IP:        "158.64.1.29",
			Name:      "Restena",
			Country:   "Luxembourg",
			City:      "Luxembourg City",
			Continent: "Europe",
		},
		{
			IP:        "193.110.81.0",
			Name:      "dns0.eu Primary",
			Country:   "France",
			City:      "Paris",
			Continent: "Europe",
		},

		// ----------------------------------------------------------------
		// Asia
		// ----------------------------------------------------------------
		{
			IP:        "223.5.5.5",
			Name:      "AliDNS Primary",
			Country:   "China",
			City:      "Hangzhou",
			Continent: "Asia",
		},
		{
			IP:        "223.6.6.6",
			Name:      "AliDNS Secondary",
			Country:   "China",
			City:      "Hangzhou",
			Continent: "Asia",
		},
		{
			IP:        "114.114.114.114",
			Name:      "114DNS Primary",
			Country:   "China",
			City:      "Nanjing",
			Continent: "Asia",
		},
		{
			IP:        "119.29.29.29",
			Name:      "DNSPod/Tencent Primary",
			Country:   "China",
			City:      "Shenzhen",
			Continent: "Asia",
		},
		{
			IP:        "168.126.63.1",
			Name:      "KT Primary",
			Country:   "South Korea",
			City:      "Seoul",
			Continent: "Asia",
		},
		{
			IP:        "168.126.63.2",
			Name:      "KT Secondary",
			Country:   "South Korea",
			City:      "Seoul",
			Continent: "Asia",
		},
		{
			IP:        "210.220.163.82",
			Name:      "SK Broadband Primary",
			Country:   "South Korea",
			City:      "Seoul",
			Continent: "Asia",
		},
		{
			IP:        "210.130.0.1",
			Name:      "IIJ Primary",
			Country:   "Japan",
			City:      "Tokyo",
			Continent: "Asia",
		},
		{
			IP:        "202.12.27.33",
			Name:      "SoftBank Primary",
			Country:   "Japan",
			City:      "Tokyo",
			Continent: "Asia",
		},
		{
			IP:        "101.101.101.101",
			Name:      "TWNIC Primary",
			Country:   "Taiwan",
			City:      "Taipei",
			Continent: "Asia",
		},
		{
			IP:        "213.59.0.1",
			Name:      "Beeline Primary",
			Country:   "Russia",
			City:      "Moscow",
			Continent: "Asia",
		},

		// ----------------------------------------------------------------
		// Oceania
		// ----------------------------------------------------------------
		{
			IP:        "139.130.4.5",
			Name:      "Telstra Primary",
			Country:   "Australia",
			City:      "Sydney",
			Continent: "Oceania",
		},
		{
			IP:        "216.146.35.35",
			Name:      "Oracle Dyn Primary",
			Country:   "Australia",
			City:      "Anycast",
			Continent: "Oceania",
		},
		{
			IP:        "203.6.241.1",
			Name:      "Optus Primary",
			Country:   "Australia",
			City:      "Sydney",
			Continent: "Oceania",
		},
		{
			IP:        "202.27.176.1",
			Name:      "Orcon NZ Primary",
			Country:   "New Zealand",
			City:      "Auckland",
			Continent: "Oceania",
		},

		// ----------------------------------------------------------------
		// South America
		// ----------------------------------------------------------------
		{
			IP:        "200.225.197.37",
			Name:      "Claro Brazil Primary",
			Country:   "Brazil",
			City:      "São Paulo",
			Continent: "South America",
		},
		{
			IP:        "200.192.96.10",
			Name:      "CPS Brazil Primary",
			Country:   "Brazil",
			City:      "Curitiba",
			Continent: "South America",
		},
		{
			IP:        "200.248.178.54",
			Name:      "EMBRATEL Primary",
			Country:   "Brazil",
			City:      "Rio de Janeiro",
			Continent: "South America",
		},

		// ----------------------------------------------------------------
		// Africa
		// ----------------------------------------------------------------
		{
			IP:        "197.157.0.10",
			Name:      "Liquid Telecom Primary",
			Country:   "Kenya",
			City:      "Nairobi",
			Continent: "Africa",
		},
		{
			IP:        "196.22.145.34",
			Name:      "Rain Primary",
			Country:   "South Africa",
			City:      "Cape Town",
			Continent: "Africa",
		},
		{
			IP:        "41.190.3.1",
			Name:      "MTN Nigeria Primary",
			Country:   "Nigeria",
			City:      "Lagos",
			Continent: "Africa",
		},

		// ----------------------------------------------------------------
		// Middle East
		// ----------------------------------------------------------------
		{
			IP:        "213.42.20.20",
			Name:      "Etisalat Primary",
			Country:   "UAE",
			City:      "Dubai",
			Continent: "Middle East",
		},
		{
			IP:        "195.175.39.39",
			Name:      "Turk Telekom Primary",
			Country:   "Turkey",
			City:      "Ankara",
			Continent: "Middle East",
		},
		{
			IP:        "212.118.241.1",
			Name:      "STC Primary",
			Country:   "Saudi Arabia",
			City:      "Riyadh",
			Continent: "Middle East",
		},
	}

	slog.Debug("resolver: curated list initialised",
		slog.Int("count", len(resolvers)),
	)

	return resolvers
}

// All returns the full curated list of public DNS resolvers. The returned
// slice is a copy so callers may safely modify it without affecting the
// package-level list.
func All() []Resolver {
	slog.Debug("resolver: All() called", slog.Int("count", len(all)))

	out := make([]Resolver, len(all))
	copy(out, all)
	return out
}

// ByContinent groups the provided resolvers by their Continent field and
// returns a map keyed by continent name. Resolvers with an empty Continent
// field are grouped under the key "Unknown".
func ByContinent(resolvers []Resolver) map[string][]Resolver {
	slog.Debug("resolver: ByContinent() called", slog.Int("input_count", len(resolvers)))

	groups := make(map[string][]Resolver)

	for _, r := range resolvers {
		key := r.Continent
		if key == "" {
			key = "Unknown"
		}
		groups[key] = append(groups[key], r)
	}

	for continent, group := range groups {
		slog.Debug("resolver: continent group built",
			slog.String("continent", continent),
			slog.Int("count", len(group)),
		)
	}

	slog.Info("resolver: ByContinent() complete",
		slog.Int("continents", len(groups)),
		slog.Int("total_resolvers", len(resolvers)),
	)

	return groups
}
