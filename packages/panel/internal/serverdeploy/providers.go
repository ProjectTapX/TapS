// Package serverdeploy resolves Minecraft server downloads. Two
// backends are supported, switchable at runtime via SetSource():
//
//   - "fastmirror" (default): goes through download.fastmirror.net, the
//     de-facto Chinese MC downloads mirror. Works inside mainland
//     China where the upstream APIs (Mojang piston-meta, PurpurMC,
//     etc.) are usually unreachable. Doesn't carry NeoForge.
//   - "official": queries each upstream directly (Mojang manifest,
//     PaperMC API, PurpurMC API, FabricMC meta, Forge promotions,
//     NeoForge maven). Use this when the panel has unrestricted
//     internet access — surfaces the freshest versions for projects
//     that haven't synced to FastMirror yet.
//
// Lists are cached per-source for 5 minutes so the typical "open
// dropdown, pick version, pick build" interaction doesn't fan out to
// N upstream hits.
package serverdeploy

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const userAgent = "TapS-panel/1 (+server-deploy)"
const fastMirrorBase = "https://download.fastmirror.net"

var httpClient = &http.Client{Timeout: 15 * time.Second}

// activeSource holds the currently selected backend.
var activeSource atomic.Value // string

// SetSource lets the settings layer push the selected backend at
// startup and on user save. Unknown values fall back to "fastmirror".
func SetSource(s string) {
	if s != "fastmirror" && s != "official" {
		s = "fastmirror"
	}
	activeSource.Store(s)
}

// Source returns the current backend ("fastmirror" or "official").
func Source() string {
	if v, ok := activeSource.Load().(string); ok {
		return v
	}
	return "fastmirror"
}

func init() { activeSource.Store("fastmirror") }

// Provider knows how to enumerate versions / builds and resolve a
// final (downloadURL, downloadName, postInstallCmd, launchArgs) tuple
// for one server type.
type Provider interface {
	ID() string
	DisplayName() string
	HasBuilds() bool
	NeedsImage() bool
	Versions() ([]string, error)
	Builds(version string) ([]string, error)
	Resolve(version, build string) (Resolved, error)
}

// Resolved is what we hand to the daemon's deploy.Manager.
type Resolved struct {
	URL            string
	FileName       string
	PostInstallCmd string
	LaunchArgs     []string
}

// ----- registry -----

type providerEntry struct {
	id           string
	display      string
	installer    bool
	hasBuilds    bool
	availableOn  []string // which sources expose this provider
	fmName       string   // FastMirror project name (when on fastmirror)
	officialImpl Provider // backing Provider for the "official" source
}

var entries = []*providerEntry{
	{id: "vanilla", display: "Vanilla", availableOn: []string{"fastmirror", "official"}, fmName: "Vanilla",
		officialImpl: &vanillaUpstream{}},
	{id: "paper", display: "Paper", hasBuilds: true, availableOn: []string{"fastmirror", "official"}, fmName: "Paper",
		officialImpl: &paperLikeUpstream{id: "paper", display: "Paper", base: "https://api.papermc.io/v2/projects/paper"}},
	{id: "purpur", display: "Purpur", availableOn: []string{"fastmirror", "official"}, fmName: "Purpur",
		officialImpl: &paperLikeUpstream{id: "purpur", display: "Purpur", base: "https://api.purpurmc.org/v2/purpur"}},
	{id: "fabric", display: "Fabric", availableOn: []string{"fastmirror", "official"}, fmName: "Fabric",
		officialImpl: &fabricUpstream{}},
	{id: "folia", display: "Folia", hasBuilds: true, availableOn: []string{"fastmirror", "official"}, fmName: "Folia",
		officialImpl: &paperLikeUpstream{id: "folia", display: "Folia", base: "https://api.papermc.io/v2/projects/folia"}},
}

// dispatcher implements Provider on top of an entry, picking the right
// backend at call time so the user can switch in settings without
// restarting anything.
type dispatcher struct{ e *providerEntry }

func (d *dispatcher) ID() string          { return d.e.id }
func (d *dispatcher) DisplayName() string { return d.e.display }
func (d *dispatcher) HasBuilds() bool     { return d.e.hasBuilds }
func (d *dispatcher) NeedsImage() bool    { return d.e.installer }

func (d *dispatcher) backend() (Provider, error) {
	src := Source()
	supported := false
	for _, s := range d.e.availableOn {
		if s == src {
			supported = true
			break
		}
	}
	if !supported {
		return nil, fmt.Errorf("%s is not available on the %q source; switch source in settings", d.e.display, src)
	}
	if src == "fastmirror" {
		return &fmProvider{id: d.e.id, display: d.e.display, fmName: d.e.fmName, installer: d.e.installer, hasBuilds: d.e.hasBuilds}, nil
	}
	if d.e.officialImpl == nil {
		return nil, fmt.Errorf("%s has no official backend", d.e.display)
	}
	return d.e.officialImpl, nil
}

func (d *dispatcher) Versions() ([]string, error) {
	b, err := d.backend()
	if err != nil {
		return nil, err
	}
	return b.Versions()
}
func (d *dispatcher) Builds(v string) ([]string, error) {
	b, err := d.backend()
	if err != nil {
		return nil, err
	}
	return b.Builds(v)
}
func (d *dispatcher) Resolve(v, build string) (Resolved, error) {
	b, err := d.backend()
	if err != nil {
		return Resolved{}, err
	}
	// Strict input validation before any URL is built. Several
	// providers (paper / fastmirror / fabric / forge / neoforge)
	// fmt.Sprintf user-supplied version+build directly into the
	// upstream URL path; without this guard a crafted version like
	// "1.21.4/../../foo" could traverse the URL inside the locked
	// host. We require both fields to come from the lists the
	// provider itself returns. Vanilla already self-validates but
	// pays the (cached) double-check, which is cheap.
	if err := validateChoices(b, v, build); err != nil {
		return Resolved{}, err
	}
	return b.Resolve(v, build)
}

// validateChoices rejects version / build values that aren't actually
// present in the provider's enumerated lists. Forge appends a human-
// readable suffix to its build labels (e.g. "47.2.0 (recommended)")
// which Resolve strips before use; both the labelled form and the
// stripped form are accepted to avoid breaking callers that pass back
// the value they got from the Builds() listing.
func validateChoices(p Provider, version, build string) error {
	if version == "" {
		// Most providers can run with an empty version (auto-pick
		// latest); leave that decision to Resolve, only validate
		// when the caller actually pinned a value.
		return nil
	}
	versions, err := p.Versions()
	if err != nil {
		return fmt.Errorf("validate version: %w", err)
	}
	if !sliceContains(versions, version) {
		return fmt.Errorf("invalid version %q (not in upstream catalog)", version)
	}
	if build == "" || !p.HasBuilds() {
		return nil
	}
	builds, err := p.Builds(version)
	if err != nil {
		return fmt.Errorf("validate build: %w", err)
	}
	for _, candidate := range builds {
		bare := candidate
		if i := strings.Index(candidate, " "); i > 0 {
			// strip " (recommended)" / " (latest)" suffix used by forge
			bare = candidate[:i]
		}
		if candidate == build || bare == build {
			return nil
		}
	}
	return fmt.Errorf("invalid build %q for version %q", build, version)
}

func sliceContains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// All returns every provider available under the current source.
func All() []Provider {
	src := Source()
	out := []Provider{}
	for _, e := range entries {
		for _, s := range e.availableOn {
			if s == src {
				out = append(out, &dispatcher{e: e})
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return providerOrder(out[i].ID()) < providerOrder(out[j].ID()) })
	return out
}

func providerOrder(id string) int {
	switch id {
	case "vanilla":
		return 0
	case "paper":
		return 1
	case "purpur":
		return 2
	case "fabric":
		return 3
	case "folia":
		return 4
	case "forge":
		return 5
	case "neoforge":
		return 6
	}
	return 99
}

// Get returns one provider by id (under the current source).
func Get(id string) (Provider, bool) {
	for _, e := range entries {
		if e.id == id {
			d := &dispatcher{e: e}
			// Resolve once to verify availability under the current source.
			if _, err := d.backend(); err != nil {
				return nil, false
			}
			return d, true
		}
	}
	return nil, false
}

// ----- caching helper -----

type cacheEntry struct {
	v   any
	exp time.Time
}

var cacheMu sync.Mutex
var cache = map[string]cacheEntry{}

const cacheTTL = 5 * time.Minute

func cached(key string, fn func() (any, error)) (any, error) {
	// Source-scope the key so switching backends doesn't reuse stale
	// version lists from the other one.
	key = Source() + ":" + key
	cacheMu.Lock()
	if e, ok := cache[key]; ok && time.Now().Before(e.exp) {
		cacheMu.Unlock()
		return e.v, nil
	}
	cacheMu.Unlock()
	v, err := fn()
	if err != nil {
		return nil, err
	}
	cacheMu.Lock()
	cache[key] = cacheEntry{v: v, exp: time.Now().Add(cacheTTL)}
	cacheMu.Unlock()
	return v, nil
}

func httpGetJSON(url string, out any) error {
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("User-Agent", userAgent)
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("upstream %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func httpGetBytes(url string) ([]byte, error) {
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("User-Agent", userAgent)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("upstream %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
}

var defaultJavaArgs = []string{"java", "-Xmx${MEM}", "-jar", "server.jar", "nogui"}

// ===== FastMirror backend =====

type fmProvider struct {
	id        string
	display   string
	fmName    string
	installer bool
	hasBuilds bool
}

func (p *fmProvider) ID() string          { return p.id }
func (p *fmProvider) DisplayName() string { return p.display }
func (p *fmProvider) HasBuilds() bool     { return p.hasBuilds }
func (p *fmProvider) NeedsImage() bool    { return p.installer }

type fmInfo struct {
	Data struct {
		Name       string   `json:"name"`
		McVersions []string `json:"mc_versions"`
	} `json:"data"`
}

func (p *fmProvider) Versions() ([]string, error) {
	v, err := cached("fm:"+p.id+":versions", func() (any, error) {
		var info fmInfo
		if err := httpGetJSON(fastMirrorBase+"/api/v3/"+p.fmName, &info); err != nil {
			return nil, err
		}
		return info.Data.McVersions, nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]string), nil
}

type fmBuild struct {
	Name        string `json:"name"`
	McVersion   string `json:"mc_version"`
	CoreVersion string `json:"core_version"`
	UpdateTime  string `json:"update_time"`
	SHA1        string `json:"sha1"`
}

type fmBuildsResp struct {
	Data struct {
		Builds []fmBuild `json:"builds"`
		Count  int       `json:"count"`
	} `json:"data"`
}

func (p *fmProvider) Builds(version string) ([]string, error) {
	v, err := cached("fm:"+p.id+":builds:"+version, func() (any, error) {
		var r fmBuildsResp
		url := fmt.Sprintf("%s/api/v3/%s/%s?offset=0&limit=50", fastMirrorBase, p.fmName, version)
		if err := httpGetJSON(url, &r); err != nil {
			return nil, err
		}
		out := make([]string, 0, len(r.Data.Builds))
		for _, b := range r.Data.Builds {
			out = append(out, b.CoreVersion)
		}
		return out, nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]string), nil
}

func (p *fmProvider) Resolve(version, build string) (Resolved, error) {
	if version == "" {
		return Resolved{}, errors.New("version required")
	}
	if build == "" {
		bs, err := p.Builds(version)
		if err != nil {
			return Resolved{}, err
		}
		if len(bs) == 0 {
			return Resolved{}, fmt.Errorf("%s has no builds for %s", p.display, version)
		}
		build = bs[0]
	}
	url := fmt.Sprintf("%s/download/%s/%s/%s", fastMirrorBase, p.fmName, version, build)
	if p.installer {
		jar := fmt.Sprintf("%s-%s-installer.jar", strings.ToLower(p.id), build)
		return Resolved{
			URL: url, FileName: jar,
			PostInstallCmd: "java -jar " + jar + " --installServer && rm -f " + jar,
			LaunchArgs:     []string{"sh", "run.sh", "nogui"},
		}, nil
	}
	return Resolved{URL: url, FileName: "server.jar", LaunchArgs: defaultJavaArgs}, nil
}

// ===== Official upstream backends =====

// ----- vanilla (Mojang piston-meta) -----

type vanillaUpstream struct{}

func (vanillaUpstream) ID() string          { return "vanilla" }
func (vanillaUpstream) DisplayName() string { return "Vanilla" }
func (vanillaUpstream) HasBuilds() bool     { return false }
func (vanillaUpstream) NeedsImage() bool    { return false }

type mojangManifest struct {
	Latest   struct{ Release, Snapshot string } `json:"latest"`
	Versions []struct {
		ID, Type, URL string
	} `json:"versions"`
}

const mojangManifestURL = "https://piston-meta.mojang.com/mc/game/version_manifest_v2.json"

func (vanillaUpstream) manifest() (*mojangManifest, error) {
	v, err := cached("vanilla:manifest", func() (any, error) {
		var m mojangManifest
		if err := httpGetJSON(mojangManifestURL, &m); err != nil {
			return nil, err
		}
		return &m, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*mojangManifest), nil
}

func (p vanillaUpstream) Versions() ([]string, error) {
	m, err := p.manifest()
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, v := range m.Versions {
		if v.Type == "release" {
			out = append(out, v.ID)
		}
	}
	return out, nil
}
func (vanillaUpstream) Builds(string) ([]string, error) { return nil, nil }
func (p vanillaUpstream) Resolve(version, _ string) (Resolved, error) {
	m, err := p.manifest()
	if err != nil {
		return Resolved{}, err
	}
	for _, v := range m.Versions {
		if v.ID != version {
			continue
		}
		var pkg struct {
			Downloads struct {
				Server struct{ URL string } `json:"server"`
			} `json:"downloads"`
		}
		if err := httpGetJSON(v.URL, &pkg); err != nil {
			return Resolved{}, err
		}
		if pkg.Downloads.Server.URL == "" {
			return Resolved{}, errors.New("no server jar for this vanilla version")
		}
		return Resolved{URL: pkg.Downloads.Server.URL, FileName: "server.jar", LaunchArgs: defaultJavaArgs}, nil
	}
	return Resolved{}, fmt.Errorf("vanilla version %q not found", version)
}

// ----- paper / purpur / folia (PaperMC v2-style API) -----

type paperLikeUpstream struct{ id, display, base string }

func (p paperLikeUpstream) ID() string          { return p.id }
func (p paperLikeUpstream) DisplayName() string { return p.display }
func (p paperLikeUpstream) HasBuilds() bool     { return true }
func (paperLikeUpstream) NeedsImage() bool      { return false }

func (p paperLikeUpstream) Versions() ([]string, error) {
	v, err := cached(p.id+":versions", func() (any, error) {
		var r struct{ Versions []string }
		if err := httpGetJSON(p.base, &r); err != nil {
			return nil, err
		}
		out := make([]string, len(r.Versions))
		for i, v := range r.Versions {
			out[len(r.Versions)-1-i] = v
		}
		return out, nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]string), nil
}

func (p paperLikeUpstream) Builds(version string) ([]string, error) {
	v, err := cached(p.id+":builds:"+version, func() (any, error) {
		var r struct{ Builds []int }
		if err := httpGetJSON(p.base+"/versions/"+version, &r); err != nil {
			return nil, err
		}
		out := make([]string, len(r.Builds))
		for i, b := range r.Builds {
			out[len(r.Builds)-1-i] = fmt.Sprintf("%d", b)
		}
		return out, nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]string), nil
}

func (p paperLikeUpstream) Resolve(version, build string) (Resolved, error) {
	if build == "" {
		bs, err := p.Builds(version)
		if err != nil {
			return Resolved{}, err
		}
		if len(bs) == 0 {
			return Resolved{}, fmt.Errorf("%s has no builds for %s", p.display, version)
		}
		build = bs[0]
	}
	var meta struct {
		Downloads struct {
			Application struct{ Name string } `json:"application"`
		} `json:"downloads"`
	}
	url := fmt.Sprintf("%s/versions/%s/builds/%s", p.base, version, build)
	if err := httpGetJSON(url, &meta); err != nil {
		return Resolved{}, err
	}
	name := meta.Downloads.Application.Name
	if name == "" {
		return Resolved{}, fmt.Errorf("%s build %s has no application download", p.display, build)
	}
	return Resolved{
		URL:        fmt.Sprintf("%s/versions/%s/builds/%s/downloads/%s", p.base, version, build, name),
		FileName:   "server.jar",
		LaunchArgs: defaultJavaArgs,
	}, nil
}

// ----- fabric (FabricMC meta) -----

type fabricUpstream struct{}

func (fabricUpstream) ID() string          { return "fabric" }
func (fabricUpstream) DisplayName() string { return "Fabric" }
func (fabricUpstream) HasBuilds() bool     { return false }
func (fabricUpstream) NeedsImage() bool    { return false }

func (fabricUpstream) Versions() ([]string, error) {
	v, err := cached("fabric:versions", func() (any, error) {
		var arr []struct {
			Version string
			Stable  bool
		}
		if err := httpGetJSON("https://meta.fabricmc.net/v2/versions/game", &arr); err != nil {
			return nil, err
		}
		out := []string{}
		for _, v := range arr {
			if v.Stable {
				out = append(out, v.Version)
			}
		}
		return out, nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]string), nil
}
func (fabricUpstream) Builds(string) ([]string, error) { return nil, nil }
func (fabricUpstream) Resolve(version, _ string) (Resolved, error) {
	var loaders []struct {
		Loader struct{ Version string }
	}
	if err := httpGetJSON("https://meta.fabricmc.net/v2/versions/loader/"+version, &loaders); err != nil {
		return Resolved{}, err
	}
	if len(loaders) == 0 {
		return Resolved{}, fmt.Errorf("fabric: no loader for %s", version)
	}
	loader := loaders[0].Loader.Version
	var installers []struct {
		Version string
		Stable  bool
	}
	if err := httpGetJSON("https://meta.fabricmc.net/v2/versions/installer", &installers); err != nil {
		return Resolved{}, err
	}
	installer := ""
	for _, i := range installers {
		if i.Stable {
			installer = i.Version
			break
		}
	}
	if installer == "" && len(installers) > 0 {
		installer = installers[0].Version
	}
	if installer == "" {
		return Resolved{}, errors.New("fabric: no installer version found")
	}
	return Resolved{
		URL:        fmt.Sprintf("https://meta.fabricmc.net/v2/versions/loader/%s/%s/%s/server/jar", version, loader, installer),
		FileName:   "server.jar",
		LaunchArgs: defaultJavaArgs,
	}, nil
}

// ----- forge -----

type forgeUpstream struct{}

func (forgeUpstream) ID() string          { return "forge" }
func (forgeUpstream) DisplayName() string { return "Forge (installer)" }
func (forgeUpstream) HasBuilds() bool     { return true }
func (forgeUpstream) NeedsImage() bool    { return true }

type forgePromos struct {
	Promos map[string]string `json:"promos"`
}

func (forgeUpstream) promos() (*forgePromos, error) {
	v, err := cached("forge:promos", func() (any, error) {
		var p forgePromos
		if err := httpGetJSON("https://files.minecraftforge.net/net/minecraftforge/forge/promotions_slim.json", &p); err != nil {
			return nil, err
		}
		return &p, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*forgePromos), nil
}
func (p forgeUpstream) Versions() ([]string, error) {
	pr, err := p.promos()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	out := []string{}
	for k := range pr.Promos {
		i := strings.LastIndex(k, "-")
		if i < 0 {
			continue
		}
		v := k[:i]
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(out)))
	return out, nil
}
func (p forgeUpstream) Builds(version string) ([]string, error) {
	pr, err := p.promos()
	if err != nil {
		return nil, err
	}
	out := []string{}
	if v, ok := pr.Promos[version+"-recommended"]; ok {
		out = append(out, v+" (recommended)")
	}
	if v, ok := pr.Promos[version+"-latest"]; ok {
		out = append(out, v+" (latest)")
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("forge: no promoted build for %s", version)
	}
	return out, nil
}
func (p forgeUpstream) Resolve(version, build string) (Resolved, error) {
	if build == "" {
		bs, err := p.Builds(version)
		if err != nil {
			return Resolved{}, err
		}
		if len(bs) == 0 {
			return Resolved{}, fmt.Errorf("forge: no build for %s", version)
		}
		build = bs[0]
	}
	if i := strings.Index(build, " "); i > 0 {
		build = build[:i]
	}
	full := version + "-" + build
	jar := fmt.Sprintf("forge-%s-installer.jar", full)
	url := fmt.Sprintf("https://maven.minecraftforge.net/net/minecraftforge/forge/%s/%s", full, jar)
	return Resolved{
		URL: url, FileName: jar,
		PostInstallCmd: "java -jar " + jar + " --installServer && rm -f " + jar,
		LaunchArgs:     []string{"sh", "run.sh", "nogui"},
	}, nil
}

// ----- neoforge -----

type neoforgeUpstream struct{}

func (neoforgeUpstream) ID() string          { return "neoforge" }
func (neoforgeUpstream) DisplayName() string { return "NeoForge (installer)" }
func (neoforgeUpstream) HasBuilds() bool     { return true }
func (neoforgeUpstream) NeedsImage() bool    { return true }

func (neoforgeUpstream) all() ([]string, error) {
	v, err := cached("neoforge:all", func() (any, error) {
		raw, err := httpGetBytes("https://maven.neoforged.net/releases/net/neoforged/neoforge/maven-metadata.xml")
		if err != nil {
			return nil, err
		}
		var x struct {
			Versioning struct {
				Versions struct {
					Version []string `xml:"version"`
				} `xml:"versions"`
			} `xml:"versioning"`
		}
		if err := xml.Unmarshal(raw, &x); err != nil {
			return nil, err
		}
		return x.Versioning.Versions.Version, nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]string), nil
}

func neoforgeMC(version string) string {
	parts := strings.SplitN(version, ".", 3)
	if len(parts) < 2 {
		return ""
	}
	return "1." + parts[0] + "." + parts[1]
}

func (p neoforgeUpstream) Versions() ([]string, error) {
	all, err := p.all()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	out := []string{}
	for _, v := range all {
		mc := neoforgeMC(v)
		if mc == "" || seen[mc] {
			continue
		}
		seen[mc] = true
		out = append(out, mc)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(out)))
	return out, nil
}
func (p neoforgeUpstream) Builds(version string) ([]string, error) {
	all, err := p.all()
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, v := range all {
		if neoforgeMC(v) == version {
			out = append(out, v)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(out)))
	if len(out) == 0 {
		return nil, fmt.Errorf("neoforge: no build for mc %s", version)
	}
	return out, nil
}
func (p neoforgeUpstream) Resolve(_, build string) (Resolved, error) {
	if build == "" {
		return Resolved{}, errors.New("neoforge: build (full version) required")
	}
	jar := fmt.Sprintf("neoforge-%s-installer.jar", build)
	url := fmt.Sprintf("https://maven.neoforged.net/releases/net/neoforged/neoforge/%s/%s", build, jar)
	return Resolved{
		URL: url, FileName: jar,
		PostInstallCmd: "java -jar " + jar + " --installServer && rm -f " + jar,
		LaunchArgs:     []string{"sh", "run.sh", "nogui"},
	}, nil
}
