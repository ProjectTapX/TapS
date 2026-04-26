package protocol

import "encoding/json"

// MessageType identifies the kind of frame on the Panel<->Daemon WebSocket.
type MessageType string

const (
	TypeRequest  MessageType = "request"
	TypeResponse MessageType = "response"
	TypeEvent    MessageType = "event"
)

// Message is the JSON-RPC-like envelope used between Panel and Daemon.
type Message struct {
	ID      string          `json:"id,omitempty"`
	Type    MessageType     `json:"type"`
	Action  string          `json:"action,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Hello is the first frame Panel sends after the WebSocket upgrade.
type Hello struct {
	Token   string `json:"token"`
	Version string `json:"version"`
}

type Welcome struct {
	DaemonID      string `json:"daemonId"`
	Version       string `json:"version"`
	OS            string `json:"os"`
	Arch          string `json:"arch"`
	RequireDocker bool   `json:"requireDocker"` // when true, only type=docker instances are allowed
	DockerReady   bool   `json:"dockerReady"`   // whether docker CLI is available on the daemon host
}

// ----- instance payloads -----

// InstanceConfig is the persistent configuration for one managed process.
type InstanceConfig struct {
	UUID       string   `json:"uuid"`
	Name       string   `json:"name"`
	Type       string   `json:"type"`        // "generic" | "minecraft" | "docker" | ...
	WorkingDir string   `json:"workingDir"`  // relative to daemon data dir if not absolute
	Command    string   `json:"command"`     // shell command line; for type=docker, the image
	Args       []string `json:"args,omitempty"`
	StopCmd    string   `json:"stopCmd"`     // text written to stdin to gracefully stop (e.g. "stop")
	AutoStart  bool     `json:"autoStart"`
	AutoRestart  bool   `json:"autoRestart"`
	RestartDelay int    `json:"restartDelay"` // seconds; 0 → 5
	// CreatedAt is the unix timestamp when this instance was first created
	// on the daemon. Used by the panel to sort listings stably; older
	// instances missing this field render as 0 and sort to the end.
	CreatedAt int64 `json:"createdAt,omitempty"`

	// OutputEncoding controls byte-level conversion before publishing to the
	// terminal stream. Empty / "utf-8" means pass-through. "gbk" decodes Chinese
	// Windows MC server output to UTF-8 so the browser renders correctly.
	OutputEncoding string `json:"outputEncoding,omitempty"`

	// Minecraft-only: server query port (default 25565). Used by the in-game
	// player list (Server List Ping protocol).
	MinecraftHost string `json:"minecraftHost,omitempty"` // default 127.0.0.1
	MinecraftPort int    `json:"minecraftPort,omitempty"` // default 25565

	// Docker-only knobs
	DockerEnv     []string `json:"dockerEnv,omitempty"`     // KEY=VAL
	DockerVolumes []string `json:"dockerVolumes,omitempty"` // host:container[:mode]
	DockerPorts   []string `json:"dockerPorts,omitempty"`   // host:container[/proto]
	DockerCPU     string   `json:"dockerCpu,omitempty"`     // e.g. "1.5"
	DockerMemory  string   `json:"dockerMemory,omitempty"`  // e.g. "2g"
	DockerDiskSize string  `json:"dockerDiskSize,omitempty"` // e.g. "10g". When set on a docker instance, daemon auto-creates a managed loopback volume of this size and mounts it at /data inside the container.

	// ManagedVolume is the name of the managed loopback volume that was
	// auto-created for this instance (when DockerDiskSize was set at create
	// time). Daemon uses it to remove the volume on Delete.
	ManagedVolume string `json:"managedVolume,omitempty"`

	// AutoDataDir is true when daemon allocated a per-instance /data directory
	// for this docker instance (no loopback, just a host folder under
	// <DataDir>/volumes/inst-<short>/). Daemon will clean it up on Delete.
	AutoDataDir bool `json:"autoDataDir,omitempty"`

	// CompletionWords is an optional per-instance vocabulary the panel's
	// terminal feeds to its Tab-completion engine. The daemon doesn't use
	// it — it's just persisted alongside the rest of the cfg.
	CompletionWords []string `json:"completionWords,omitempty"`

	// Hibernation: "auto-stop on idle then wake on connect". Three-state
	// enable so the user can either follow the panel-wide default
	// (`HibernationEnabled == nil`) or override per instance. Idle
	// minutes of 0 means "use the global default".
	HibernationEnabled     *bool `json:"hibernationEnabled,omitempty"`
	HibernationIdleMinutes int   `json:"hibernationIdleMinutes,omitempty"`
	// HibernationActive is daemon-managed runtime state — true means the
	// daemon currently has a fake-SLP listener bound to the host port and
	// the real container is stopped. Persisted so a daemon restart can
	// resume the listener instead of leaving the instance dark.
	HibernationActive bool `json:"hibernationActive,omitempty"`
}

// InstanceStatus is reported back to the Panel.
type InstanceStatus string

const (
	StatusStopped     InstanceStatus = "stopped"
	StatusStarting    InstanceStatus = "starting"
	StatusRunning     InstanceStatus = "running"
	StatusStopping    InstanceStatus = "stopping"
	StatusCrashed     InstanceStatus = "crashed"
	StatusHibernating InstanceStatus = "hibernating"
)

type InstanceInfo struct {
	Config   InstanceConfig `json:"config"`
	Status   InstanceStatus `json:"status"`
	PID      int            `json:"pid"`
	ExitCode int            `json:"exitCode"`
}

type InstanceTarget struct {
	UUID string `json:"uuid"`
}

type InstanceInputReq struct {
	UUID string `json:"uuid"`
	Data string `json:"data"`
}

type InstanceOutputEvent struct {
	UUID string `json:"uuid"`
	Data string `json:"data"`
}

// InstanceOutputHistoryResp is the daemon's reply to instance.outputHistory:
// the cached terminal scrollback (UTF-8) for one instance.
type InstanceOutputHistoryResp struct {
	UUID    string `json:"uuid"`
	History string `json:"history"`
}

// DockerStatsAllResp is the daemon's reply to instance.dockerStatsAll —
// one Stats entry per running container, fetched in a single shell-out so
// the panel can populate dashboards in one round-trip per daemon instead
// of one per instance.
type DockerStatsAllResp struct {
	Items []DockerStatsResp `json:"items"`
}

// DockerStatsResp is what the daemon returns from `docker stats --no-stream`
// for a single container. Running=false when the container isn't there.
type DockerStatsResp struct {
	Name            string  `json:"name"`
	Running         bool    `json:"running"`
	MemBytes        int64   `json:"memBytes"`
	MemLimit        int64   `json:"memLimit"`
	MemPercent      float64 `json:"memPercent"`
	CPUPercent      float64 `json:"cpuPercent"`
	NetRxBytes      int64   `json:"netRxBytes"`
	NetTxBytes      int64   `json:"netTxBytes"`
	BlockReadBytes  int64   `json:"blockReadBytes"`
	BlockWriteBytes int64   `json:"blockWriteBytes"`
	// DiskUsedBytes / DiskTotalBytes describe the per-instance managed
	// volume usage when one is configured. Zero if no managed volume.
	DiskUsedBytes  int64 `json:"diskUsedBytes,omitempty"`
	DiskTotalBytes int64 `json:"diskTotalBytes,omitempty"`
}

type InstanceSubscribeReq struct {
	UUID    string `json:"uuid"`
	Enabled bool   `json:"enabled"`
}

type InstanceResizeReq struct {
	UUID string `json:"uuid"`
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

// Action names (kept centralized so both sides stay in sync).
const (
	ActionInstanceList      = "instance.list"
	ActionInstanceGet       = "instance.get"
	ActionInstanceCreate    = "instance.create"
	ActionInstanceUpdate    = "instance.update"
	ActionInstanceStart     = "instance.start"
	ActionInstanceStop      = "instance.stop"
	ActionInstanceKill      = "instance.kill"
	ActionInstanceDelete    = "instance.delete"
	ActionInstanceInput     = "instance.input"
	ActionInstanceResize    = "instance.resize"
	ActionInstanceSubscribe = "instance.subscribe"
	ActionInstanceOutput    = "instance.output" // event
	ActionInstanceStatus    = "instance.status" // event
	ActionInstanceOutputHistory = "instance.outputHistory"
	ActionInstanceDockerStats   = "instance.dockerStats"
	ActionInstanceDockerStatsAll = "instance.dockerStatsAll"
	ActionMonitorSnap       = "monitor.snapshot"
	ActionFsList            = "fs.list"
	ActionFsRead            = "fs.read"
	ActionFsWrite           = "fs.write"
	ActionFsMkdir           = "fs.mkdir"
	ActionFsDelete          = "fs.delete"
	ActionFsRename          = "fs.rename"
	ActionFsCopy            = "fs.copy"
	ActionFsMove            = "fs.move"
	ActionFsZip             = "fs.zip"
	ActionFsUnzip           = "fs.unzip"
	ActionDockerImages       = "docker.images"
	ActionDockerPull         = "docker.pull"
	ActionDockerPullProgress = "docker.pullProgress" // event
	ActionDockerPullDone     = "docker.pullDone"     // event
	ActionDockerRemove       = "docker.remove"
	ActionVolumeList         = "volume.list"
	ActionVolumeCreate       = "volume.create"
	ActionVolumeRemove       = "volume.remove"
	ActionVolumeDiskInfo     = "volume.diskInfo"
	// Deploy a Minecraft server JAR (or installer) into an instance's
	// /data volume. Long-running; status is read back via Get.
	ActionInstanceDeployStart  = "instance.deployStart"
	ActionInstanceDeployStatus = "instance.deployStatus"
	ActionMcPlayers         = "mc.players"
	ActionInstancePlayersAll = "instance.playersAll"
	// Hibernation: panel pushes the latest defaults + favicon to the
	// daemon whenever an admin saves the system-settings page. Daemon
	// keeps them in memory so the per-instance watcher doesn't have to
	// reach back to panel.
	ActionHibernationConfig = "hibernation.config"
	ActionBackupList        = "backup.list"
	ActionBackupCreate      = "backup.create"
	ActionBackupRestore     = "backup.restore"
	ActionBackupDelete      = "backup.delete"
	ActionMonitorProcess    = "monitor.process"
)

// ----- minecraft -----

type McPlayersReq struct {
	UUID string `json:"uuid"`
}

type McPlayer struct {
	Name string `json:"name"`
	UUID string `json:"uuid,omitempty"`
}

type McPlayersResp struct {
	Online      bool       `json:"online"`
	Error       string     `json:"error,omitempty"`
	Description string     `json:"description,omitempty"`
	Version     string     `json:"version,omitempty"`
	Max         int        `json:"max"`
	Count       int        `json:"count"`
	Players     []McPlayer `json:"players"`
}

// PlayersBrief is the per-instance summary included in the dashboard's
// batch player-count fetch — keeps the wire format small (no full player
// list, no MOTD) so the response fits in a single round-trip even for
// daemons hosting many instances.
type PlayersBrief struct {
	UUID   string `json:"uuid"`
	Online bool   `json:"online"`
	Count  int    `json:"count"`
	Max    int    `json:"max"`
}

// PlayersAllResp is the daemon's reply to instance.playersAll: a summary
// per instance whose configured port answered the SLP ping. Instances
// that aren't running, aren't MC, or didn't respond within the deadline
// are simply omitted.
type PlayersAllResp struct {
	Items []PlayersBrief `json:"items"`
}

// HibernationConfig is the panel-pushed default + assets bundle the
// daemon's hibernation manager uses to render the fake server's MOTD,
// favicon, and kick message — and to decide which instances opt-in by
// default.
type HibernationConfig struct {
	DefaultEnabled     bool   `json:"defaultEnabled"`
	DefaultIdleMinutes int    `json:"defaultIdleMinutes"`
	// WarmupMinutes is the grace period after Start during which the idle
	// counter is held at 0. Lets a freshly-launched server finish loading
	// chunks before it can be auto-hibernated. 0 = no warmup.
	WarmupMinutes int    `json:"warmupMinutes"`
	MOTD          string `json:"motd"`
	KickMessage   string `json:"kickMessage"`
	// IconPNG is the raw PNG bytes (already validated 64×64 by panel).
	// Empty = the fake server omits favicon from the SLP response.
	IconPNG []byte `json:"iconPng,omitempty"`
}

// ----- backup -----

type BackupEntry struct {
	Name      string `json:"name"`      // file name (timestamped zip)
	Size      int64  `json:"size"`
	Created   int64  `json:"created"`   // unix seconds
	InstanceUUID string `json:"instanceUuid"`
}

type BackupListReq struct {
	UUID string `json:"uuid"`
}

type BackupListResp struct {
	Entries []BackupEntry `json:"entries"`
}

type BackupCreateReq struct {
	UUID string `json:"uuid"`
	Note string `json:"note,omitempty"` // appended to filename if non-empty
}

type BackupRestoreReq struct {
	UUID string `json:"uuid"`
	Name string `json:"name"`
}

type BackupDeleteReq struct {
	UUID string `json:"uuid"`
	Name string `json:"name"`
}

// ----- per-instance process monitor -----

type ProcessSnapshot struct {
	UUID       string  `json:"uuid"`
	PID        int     `json:"pid"`
	Running    bool    `json:"running"`
	CPUPercent float64 `json:"cpuPercent"`
	MemBytes   uint64  `json:"memBytes"`
	NumThreads int32   `json:"numThreads"`
	Timestamp  int64   `json:"timestamp"`
}

type MonitorProcessReq struct {
	UUID string `json:"uuid"`
}

// ----- fs payloads -----

type FsListReq struct {
	Path string `json:"path"`
}

type FsEntry struct {
	Name     string `json:"name"`
	IsDir    bool   `json:"isDir"`
	Size     int64  `json:"size"`
	Modified int64  `json:"modified"` // unix seconds
	Mode     string `json:"mode"`
}

type FsListResp struct {
	Path    string    `json:"path"`
	Entries []FsEntry `json:"entries"`
}

type FsReadReq struct {
	Path string `json:"path"`
}

type FsReadResp struct {
	Content string `json:"content"`
	Size    int64  `json:"size"`
}

type FsWriteReq struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type FsPathReq struct {
	Path string `json:"path"`
}

type FsRenameReq struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type FsCopyReq struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type FsMoveReq struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type FsZipReq struct {
	Paths []string `json:"paths"`
	Dest  string   `json:"dest"`
}

type FsUnzipReq struct {
	Src     string `json:"src"`
	DestDir string `json:"destDir"`
}

// ----- monitor -----

type MonitorSnapshot struct {
	CPUPercent  float64 `json:"cpuPercent"`
	MemTotal    uint64  `json:"memTotal"`
	MemUsed     uint64  `json:"memUsed"`
	MemPercent  float64 `json:"memPercent"`
	DiskTotal   uint64  `json:"diskTotal"`
	DiskUsed    uint64  `json:"diskUsed"`
	DiskPercent float64 `json:"diskPercent"`
	UptimeSec   uint64  `json:"uptimeSec"`
	Timestamp   int64   `json:"timestamp"`
}

// ----- docker -----

type DockerImage struct {
	ID          string `json:"id"`
	Repository  string `json:"repository"`
	Tag         string `json:"tag"`
	Size        int64  `json:"size"`
	Created     int64  `json:"created"`          // unix seconds
	DisplayName string `json:"displayName,omitempty"` // from OCI / taps label
	Description string `json:"description,omitempty"` // from OCI / taps label
}

type DockerImagesResp struct {
	Available bool          `json:"available"` // false if docker not installed/running
	Error     string        `json:"error,omitempty"`
	Images    []DockerImage `json:"images"`
}

type DockerPullReq struct {
	Image  string `json:"image"`
	PullID string `json:"pullId,omitempty"` // when set, daemon emits docker.pullProgress events keyed on this id
}

type DockerPullProgress struct {
	PullID string `json:"pullId"`
	Line   string `json:"line"`
}

type DockerPullDone struct {
	PullID string `json:"pullId"`
	Error  string `json:"error,omitempty"`
}

type DockerRemoveReq struct {
	ID string `json:"id"`
}

// ----- managed volumes -----

// Volume describes a fixed-size loopback volume created on the daemon host.
// The host mounts an image file at MountPath; the user can then map
// MountPath → container_path in the instance's DockerVolumes config.
type Volume struct {
	Name      string `json:"name"`
	SizeBytes int64  `json:"sizeBytes"`
	UsedBytes int64  `json:"usedBytes,omitempty"`
	FsType    string `json:"fsType"`        // ext4 / xfs / etc.
	ImagePath string `json:"imagePath"`
	MountPath string `json:"mountPath"`
	Mounted   bool   `json:"mounted"`
	CreatedAt int64  `json:"createdAt"`
}

type VolumeListResp struct {
	Available bool     `json:"available"` // false if loop devices / mkfs unavailable
	Error     string   `json:"error,omitempty"`
	Volumes   []Volume `json:"volumes"`
}

// VolumeDiskInfoResp reports the partition that hosts the daemon's
// volumes directory — used by the panel's group scheduler to bias new
// instances toward nodes with disk headroom.
type VolumeDiskInfoResp struct {
	UsedBytes  int64 `json:"usedBytes"`
	TotalBytes int64 `json:"totalBytes"`
	FreeBytes  int64 `json:"freeBytes"`
}

// DeployStartReq is the panel's pre-resolved instruction. Panel does the
// per-provider API hits and just hands the daemon a download URL plus
// (for installer-based providers like Forge) the post-download command
// to run inside a one-shot Java container.
type DeployStartReq struct {
	UUID         string `json:"uuid"`
	ProviderID   string `json:"providerId"`   // "vanilla" | "paper" | "purpur" | "fabric" | "forge" | "neoforge"
	Version      string `json:"version"`      // human-readable, e.g. "1.21.4"
	Build        string `json:"build"`        // optional, provider-specific
	DownloadURL  string `json:"downloadUrl"`  // primary file to fetch
	DownloadName string `json:"downloadName"` // saved under /data with this name
	// PostInstallCmd, if non-empty, is run inside a one-shot
	//   docker run --rm -v <vol>:/data <image> sh -c "<cmd>"
	// after the download completes. Used for Forge / NeoForge installers.
	PostInstallCmd string `json:"postInstallCmd"`
	// LaunchArgs is what we write back as the instance's Args. Typically
	// ["java","-Xmx${MEM}","-jar","server.jar","nogui"]. The daemon
	// substitutes ${MEM} from the container memory limit at apply time.
	LaunchArgs []string `json:"launchArgs"`
	// AcceptEula generates eula.txt with eula=true when set.
	AcceptEula bool `json:"acceptEula"`
}

// DeployStartResp acknowledges the kickoff. The deploy itself runs on a
// goroutine; status is polled via ActionInstanceDeployStatus.
type DeployStartResp struct {
	Started bool `json:"started"`
}

// DeployStage values are deliberately a small enum so the UI can render
// per-stage progress bars / spinners.
type DeployStage string

const (
	DeployStageQueued      DeployStage = "queued"
	DeployStageValidating  DeployStage = "validating"
	DeployStageClearing    DeployStage = "clearing"
	DeployStageDownloading DeployStage = "downloading"
	DeployStageInstalling  DeployStage = "installing"
	DeployStageConfiguring DeployStage = "configuring"
	DeployStageDone        DeployStage = "done"
	DeployStageError       DeployStage = "error"
)

// DeployStatus is the response shape for Get; safe to poll once a
// second from the browser.
type DeployStatus struct {
	UUID       string      `json:"uuid"`
	Active     bool        `json:"active"`
	Stage      DeployStage `json:"stage"`
	Percent    int         `json:"percent"` // 0..100, only meaningful for downloading/installing
	BytesDone  int64       `json:"bytesDone"`
	BytesTotal int64       `json:"bytesTotal"`
	// MessageKey is an i18n key the frontend resolves; Message is a
	// fallback / debug string. The frontend prefers MessageKey when
	// present so user-visible text stays localized.
	MessageKey string      `json:"messageKey,omitempty"`
	Message    string      `json:"message"`
	Error      string      `json:"error,omitempty"`
	StartedAt  int64       `json:"startedAt"`
	FinishedAt int64       `json:"finishedAt,omitempty"`
}

type VolumeCreateReq struct {
	Name      string `json:"name"`
	SizeBytes int64  `json:"sizeBytes"`     // total file size in bytes
	FsType    string `json:"fsType"`        // ext4 (default) | xfs
}

type VolumeRemoveReq struct {
	Name string `json:"name"`
}
