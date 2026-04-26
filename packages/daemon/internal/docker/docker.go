// Package docker shells out to the local `docker` CLI to enumerate and manage
// images. Daemon hosts without docker installed simply report Available=false.
package docker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ProjectTapX/TapS/packages/shared/protocol"
)

// Audit-2026-04-24-v3 H5: defence against the docker CLI flag-injection
// class of attacks. If a positional argument starts with "-" docker
// treats it as a global flag (`--config=/tmp/x` redirects auth, `--debug`
// enables verbose, `--host` retargets the daemon socket). All daemon
// shell-outs below either:
//   (a) prefix every variable arg with `--` so docker stops flag
//       parsing, AND
//   (b) validate the arg shape against the docker naming spec.
// Both layers are intentional — if the regex misses an edge case the
// `--` separator still neuters the attack.
var (
	// Image references: `[host[:port]/]name[:tag][@sha256:hex]`. Docker
	// itself is a bit more permissive (UTF-8 in tags) but the strict
	// ASCII subset is good enough for any real registry.
	imageRefRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._/:@-]{0,254}$`)
	// Container/image IDs: hex digests, short hashes, or simple names.
	idRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
)

// validImage rejects flag-shaped tokens and arbitrarily long input.
// Returned error has a stable shape so callers can map to apiErr.
func validImage(s string) error {
	if !imageRefRe.MatchString(s) {
		return errors.New("invalid image reference")
	}
	return nil
}

// ValidImage is the exported wrapper used by the instance package
// when validating user-supplied images at startDocker time. Same
// underlying check as the package-internal use sites.
func ValidImage(s string) error { return validImage(s) }

func validID(s string) error {
	if !idRe.MatchString(s) {
		return errors.New("invalid container/image id")
	}
	return nil
}

// Available probes whether `docker` is on PATH and the daemon is reachable.
func Available() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "docker", "version", "--format", "{{.Server.Version}}").Run() == nil
}

type rawImage struct {
	ID         string `json:"ID"`
	Repository string `json:"Repository"`
	Tag        string `json:"Tag"`
	Size       string `json:"Size"`
	CreatedAt  string `json:"CreatedAt"`
}

func List(ctx context.Context) (protocol.DockerImagesResp, error) {
	if !Available() {
		return protocol.DockerImagesResp{Available: false, Error: "docker not available on this host"}, nil
	}
	out, err := exec.CommandContext(ctx, "docker", "image", "ls", "--format", "{{json .}}", "--no-trunc").Output()
	if err != nil {
		return protocol.DockerImagesResp{Available: true, Error: errOutput(err)}, nil
	}
	resp := protocol.DockerImagesResp{Available: true, Images: []protocol.DockerImage{}}
	var ids []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var r rawImage
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			continue
		}
		resp.Images = append(resp.Images, protocol.DockerImage{
			ID:         r.ID,
			Repository: r.Repository,
			Tag:        r.Tag,
			Size:       parseSize(r.Size),
			Created:    parseDockerTime(r.CreatedAt),
		})
		ids = append(ids, r.ID)
	}
	// Batch-inspect labels for display name + description. We read
	// both the OCI standard labels and a taps-specific override so
	// users who build their own images can set a friendly name via
	// LABEL taps.displayName="My Server" in their Dockerfile.
	if len(ids) > 0 {
		labels := inspectLabels(ctx, ids)
		for i := range resp.Images {
			if lm, ok := labels[resp.Images[i].ID]; ok {
				resp.Images[i].DisplayName = lm.displayName
				resp.Images[i].Description = lm.description
			}
		}
	}
	return resp, nil
}

type labelMeta struct {
	displayName string
	description string
}

// inspectLabels batch-reads OCI + taps labels for a set of image IDs.
// Returns a map keyed by the full image ID (sha256:...).
func inspectLabels(ctx context.Context, ids []string) map[string]labelMeta {
	// Build the inspect command. --format outputs one line per image:
	//   <id>|||<taps.displayName>|||<ociTitle>|||<taps.description>|||<ociDesc>
	// Missing labels render as "<no value>" which we strip below.
	const sep = "|||"
	format := `{{.ID}}` + sep +
		`{{index .Config.Labels "taps.displayName"}}` + sep +
		`{{index .Config.Labels "org.opencontainers.image.title"}}` + sep +
		`{{index .Config.Labels "taps.description"}}` + sep +
		`{{index .Config.Labels "org.opencontainers.image.description"}}`
	args := []string{"image", "inspect", "--format", format}
	args = append(args, ids...)
	raw, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		return nil
	}
	out := map[string]labelMeta{}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		parts := strings.SplitN(line, sep, 5)
		if len(parts) < 5 {
			continue
		}
		clean := func(s string) string {
			s = strings.TrimSpace(s)
			if s == "<no value>" || s == "" {
				return ""
			}
			return s
		}
		id := clean(parts[0])
		tapsName := clean(parts[1])
		ociTitle := clean(parts[2])
		tapsDesc := clean(parts[3])
		ociDesc := clean(parts[4])
		name := tapsName
		if name == "" {
			name = ociTitle
		}
		desc := tapsDesc
		if desc == "" {
			desc = ociDesc
		}
		if name != "" || desc != "" {
			out[id] = labelMeta{displayName: name, description: desc}
		}
	}
	return out
}

// Pull is synchronous and may take a while; caller should set a generous timeout.
// If onLine is non-nil, each line of stdout/stderr is delivered to it as the
// pull progresses (one line per layer state change).
func Pull(ctx context.Context, image string, onLine func(string)) error {
	if err := validImage(image); err != nil {
		return err
	}
	// `--` halts docker's global-flag parsing so a future regex bypass
	// (or new docker flag added upstream) doesn't reopen the injection.
	cmd := exec.CommandContext(ctx, "docker", "pull", "--", image)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	scan := func(r io.Reader) {
		buf := make([]byte, 0, 256)
		one := make([]byte, 1)
		for {
			n, err := r.Read(one)
			if n > 0 {
				c := one[0]
				if c == '\n' || c == '\r' {
					if len(buf) > 0 && onLine != nil {
						onLine(string(buf))
					}
					buf = buf[:0]
				} else {
					buf = append(buf, c)
				}
			}
			if err != nil {
				if len(buf) > 0 && onLine != nil {
					onLine(string(buf))
				}
				return
			}
		}
	}
	go scan(stdout)
	go scan(stderr)
	return cmd.Wait()
}

func Remove(ctx context.Context, id string) error {
	if err := validID(id); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "docker", "image", "rm", "-f", "--", id)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return errors.New(strings.TrimSpace(string(out)))
	}
	return nil
}

// Stats runs `docker stats --no-stream` for one container and parses the
// MemUsage / MemPerc / CPUPerc / NetIO / BlockIO columns. Returns Running=false
// if the container isn't there (already exited, or never started).
func Stats(ctx context.Context, name string) protocol.DockerStatsResp {
	out := protocol.DockerStatsResp{Name: name}
	if validID(name) != nil {
		return out
	}
	cmd := exec.CommandContext(ctx, "docker", "stats", "--no-stream", "--no-trunc",
		"--format", "{{.Name}}|{{.MemUsage}}|{{.MemPerc}}|{{.CPUPerc}}|{{.NetIO}}|{{.BlockIO}}", "--", name)
	raw, err := cmd.CombinedOutput()
	if err != nil {
		return out
	}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line == "" {
			continue
		}
		s := parseStatsLine(line)
		if s.Name == name {
			return s
		}
	}
	return out
}

// StatsAll fetches docker stats for every running container in one shell-out
// — used by the panel's per-daemon dashboard so we don't have to issue N
// separate `docker stats` calls when listing many instances.
func StatsAll(ctx context.Context) []protocol.DockerStatsResp {
	cmd := exec.CommandContext(ctx, "docker", "stats", "--no-stream", "--no-trunc",
		"--format", "{{.Name}}|{{.MemUsage}}|{{.MemPerc}}|{{.CPUPerc}}|{{.NetIO}}|{{.BlockIO}}")
	raw, err := cmd.CombinedOutput()
	if err != nil {
		return nil
	}
	out := []protocol.DockerStatsResp{}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line == "" {
			continue
		}
		out = append(out, parseStatsLine(line))
	}
	return out
}

// parseStatsLine splits a single tab-formatted stats line into a populated
// DockerStatsResp. Rows always have a name when they came from `docker
// stats`, so Running is true for every line we see.
func parseStatsLine(line string) protocol.DockerStatsResp {
	parts := strings.Split(line, "|")
	if len(parts) < 4 {
		return protocol.DockerStatsResp{}
	}
	out := protocol.DockerStatsResp{Name: strings.TrimSpace(parts[0]), Running: true}
	if mu := strings.Split(parts[1], "/"); len(mu) == 2 {
		out.MemBytes = parseHumanBytes(strings.TrimSpace(mu[0]))
		out.MemLimit = parseHumanBytes(strings.TrimSpace(mu[1]))
	}
	out.MemPercent = parsePercent(parts[2])
	out.CPUPercent = parsePercent(parts[3])
	if len(parts) >= 5 {
		if io := strings.Split(parts[4], "/"); len(io) == 2 {
			out.NetRxBytes = parseHumanBytes(strings.TrimSpace(io[0]))
			out.NetTxBytes = parseHumanBytes(strings.TrimSpace(io[1]))
		}
	}
	if len(parts) >= 6 {
		if io := strings.Split(parts[5], "/"); len(io) == 2 {
			out.BlockReadBytes = parseHumanBytes(strings.TrimSpace(io[0]))
			out.BlockWriteBytes = parseHumanBytes(strings.TrimSpace(io[1]))
		}
	}
	return out
}

// parseHumanBytes accepts "125MiB", "2GiB", "1.5kB", "200B", returns bytes.
func parseHumanBytes(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	// strip optional trailing "B" / "iB"
	upper := strings.ToUpper(s)
	mult := int64(1)
	switch {
	case strings.HasSuffix(upper, "TIB"):
		mult = 1 << 40
		s = s[:len(s)-3]
	case strings.HasSuffix(upper, "GIB"):
		mult = 1 << 30
		s = s[:len(s)-3]
	case strings.HasSuffix(upper, "MIB"):
		mult = 1 << 20
		s = s[:len(s)-3]
	case strings.HasSuffix(upper, "KIB"):
		mult = 1 << 10
		s = s[:len(s)-3]
	case strings.HasSuffix(upper, "TB"):
		mult = 1000 * 1000 * 1000 * 1000
		s = s[:len(s)-2]
	case strings.HasSuffix(upper, "GB"):
		mult = 1000 * 1000 * 1000
		s = s[:len(s)-2]
	case strings.HasSuffix(upper, "MB"):
		mult = 1000 * 1000
		s = s[:len(s)-2]
	case strings.HasSuffix(upper, "KB"):
		mult = 1000
		s = s[:len(s)-2]
	case strings.HasSuffix(upper, "B"):
		s = s[:len(s)-1]
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return int64(v * float64(mult))
}

func parsePercent(s string) float64 {
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "%"))
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

func errOutput(err error) string {
	if ee, ok := err.(*exec.ExitError); ok {
		return strings.TrimSpace(string(ee.Stderr))
	}
	return err.Error()
}

// parseSize converts strings like "123MB", "4.5GB", "10kB" into bytes.
func parseSize(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	var i int
	for i < len(s) {
		c := s[i]
		if (c < '0' || c > '9') && c != '.' {
			break
		}
		i++
	}
	num, err := strconv.ParseFloat(s[:i], 64)
	if err != nil {
		return 0
	}
	unit := strings.ToLower(strings.TrimSpace(s[i:]))
	mult := float64(1)
	switch unit {
	case "b":
		mult = 1
	case "kb", "k":
		mult = 1024
	case "mb", "m":
		mult = 1024 * 1024
	case "gb", "g":
		mult = 1024 * 1024 * 1024
	case "tb", "t":
		mult = 1024 * 1024 * 1024 * 1024
	}
	return int64(num * mult)
}

// parseDockerTime parses formats like "2024-01-15 12:34:56 +0000 UTC" returning unix seconds.
func parseDockerTime(s string) int64 {
	s = strings.TrimSpace(s)
	for _, layout := range []string{
		"2006-01-02 15:04:05 -0700 MST",
		"2006-01-02 15:04:05 -0700",
		time.RFC3339,
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Unix()
		}
	}
	return 0
}
