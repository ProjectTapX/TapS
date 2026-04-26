package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/ProjectTapX/TapS/packages/daemon/internal/backup"
	"github.com/ProjectTapX/TapS/packages/daemon/internal/config"
	"github.com/ProjectTapX/TapS/packages/daemon/internal/deploy"
	"github.com/ProjectTapX/TapS/packages/daemon/internal/docker"
	dfs "github.com/ProjectTapX/TapS/packages/daemon/internal/fs"
	"github.com/ProjectTapX/TapS/packages/daemon/internal/hibernation"
	"github.com/ProjectTapX/TapS/packages/daemon/internal/instance"
	"github.com/ProjectTapX/TapS/packages/daemon/internal/minecraft"
	"github.com/ProjectTapX/TapS/packages/daemon/internal/monitor"
	"github.com/ProjectTapX/TapS/packages/daemon/internal/uploadsession"
	"github.com/ProjectTapX/TapS/packages/daemon/internal/volumes"
	"github.com/ProjectTapX/TapS/packages/shared/protocol"
	"github.com/ProjectTapX/TapS/packages/shared/ratelimit"
)

const daemonVersion = "26.1.0"

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type Server struct {
	cfg      *config.Config
	mgr      *instance.Manager
	fs       *dfs.FS
	backup   *backup.Manager
	volumes  *volumes.Manager
	hib      *hibernation.Manager
	deploy   *deploy.Manager
	daemonID string
	// Per-IP throttle on token validation failures. Anyone who fails
	// the shared-token gate too many times in a short window gets
	// banned for cfg.RateLimitBanMin minutes — covers both the HTTP
	// upload/download/backup endpoints and the WS hello.
	tokenLimit *ratelimit.Bucket
	// CertPEM, when set, is served verbatim by /cert so the panel can
	// fetch the daemon's public certificate over the same wss link
	// it just dialed (TOFU verification cross-check).
	CertPEM []byte
	// uploads tracks live chunked-upload sessions. Init returns an id
	// that every subsequent chunk request must echo back; sessions
	// time out after 1h and the GC unlinks the orphaned .partial.
	uploads *uploadsession.Manager

	// audit-2026-04-25 H2: per-session bound on concurrently-running
	// dispatch goroutines. handleWS acquires a slot before launching
	// dispatch; if the buffer is full it replies daemon.busy with the
	// original message ID so the panel can fail the call cleanly
	// instead of waiting on a request the daemon never picks up.
	// Channel capacity is set per-connection in handleWS from
	// cfg.WSDispatchConcurrency so an admin retune (config.json edit
	// + daemon restart) takes effect on the next connection.
}

func New(cfg *config.Config, mgr *instance.Manager, fs *dfs.FS, bk *backup.Manager, vm *volumes.Manager, hib *hibernation.Manager, dp *deploy.Manager) *Server {
	s := &Server{
		cfg:        cfg,
		mgr:        mgr,
		fs:         fs,
		backup:     bk,
		volumes:    vm,
		hib:        hib,
		deploy:     dp,
		daemonID:   uuid.NewString(),
		tokenLimit: ratelimit.New("daemon-token", cfg.RateLimitThreshold, time.Duration(cfg.RateLimitBanMin)*time.Minute),
	}
	s.uploads = uploadsession.New(func(sess *uploadsession.Session) {
		// Stale session: drop the .partial that nobody finalized.
		_ = os.Remove(sess.PathAbs + ".partial")
		log.Printf("upload: gc stale session id=%s path=%s", sess.ID, sess.PathAbs)
	})
	return s
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleWS)
	mux.HandleFunc("/files/upload", s.requireToken(s.handleUpload))
	mux.HandleFunc("/files/upload/init", s.requireToken(s.handleUploadInit))
	mux.HandleFunc("/files/download", s.requireToken(s.handleDownload))
	mux.HandleFunc("/backups/download", s.requireToken(s.handleBackupDownload))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	// /cert returns the daemon's public certificate in PEM form. Used
	// by the panel's "add daemon" wizard for TOFU pinning — the panel
	// fetches it over the same TLS handshake (verifying chain isn't
	// possible since the cert is self-signed, so the panel computes
	// the SHA-256 on the bytes and asks the operator to confirm).
	// Token-less by design: the cert is public information.
	mux.HandleFunc("/cert", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-pem-file")
		_, _ = w.Write(s.CertPEM)
	})
	return mux
}

// clientIP extracts the peer IP for rate-limit bucketing. Falls back to
// the raw RemoteAddr when SplitHostPort fails (unix sockets etc).
func clientIP(r *http.Request) string {
	if i := strings.LastIndex(r.RemoteAddr, ":"); i > 0 {
		return r.RemoteAddr[:i]
	}
	return r.RemoteAddr
}

func (s *Server) requireToken(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		if ok, retry := s.tokenLimit.Check(ip); !ok {
			secs := int(retry.Seconds())
			if secs < 1 {
				secs = 1
			}
			w.Header().Set("Retry-After", strconv.Itoa(secs))
			writeJSONErrorWithParams(w, http.StatusTooManyRequests,
				"daemon.rate_limited", "rate limited",
				map[string]any{"retryAfter": secs})
			return
		}
		t := r.URL.Query().Get("token")
		if t == "" {
			t = r.Header.Get("X-Daemon-Token")
		}
		if t != s.cfg.Token {
			_, backoff := s.tokenLimit.Fail(ip)
			if backoff > 0 {
				time.Sleep(backoff)
			}
			writeJSONError(w, http.StatusUnauthorized, "daemon.unauthorized", "unauthorized")
			return
		}
		s.tokenLimit.Reset(ip)
		h(w, r)
	}
}

// session keeps per-connection state (subscriptions and write mutex).
type session struct {
	conn   *websocket.Conn
	writeM sync.Mutex
	subMu  sync.Mutex
	subs   map[string]func() // uuid -> unsubscribe
}

func (sess *session) writeJSON(v any) error {
	sess.writeM.Lock()
	defer sess.writeM.Unlock()
	return sess.conn.WriteJSON(v)
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if ok, retry := s.tokenLimit.Check(ip); !ok {
		secs := int(retry.Seconds())
		if secs < 1 {
			secs = 1
		}
		w.Header().Set("Retry-After", strconv.Itoa(secs))
		writeJSONErrorWithParams(w, http.StatusTooManyRequests,
			"daemon.rate_limited", "rate limited",
			map[string]any{"retryAfter": secs})
		return
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade: %v", err)
		return
	}
	defer conn.Close()

	// Cap inbound frame size before reading anything, so a malicious
	// peer can't send a multi-GiB JSON payload (e.g. fs.write content)
	// that would force the daemon to allocate the whole buffer. Frames
	// over the cap make ReadMessage return an error, dropping the conn.
	if s.cfg.MaxWSFrameBytes > 0 {
		conn.SetReadLimit(s.cfg.MaxWSFrameBytes)
	}

	conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		log.Printf("ws read hello: %v", err)
		return
	}
	var hello protocol.Hello
	if err := json.Unmarshal(raw, &hello); err != nil || hello.Token != s.cfg.Token {
		_, backoff := s.tokenLimit.Fail(ip)
		if backoff > 0 {
			time.Sleep(backoff)
		}
		_ = conn.WriteJSON(protocol.Message{Type: protocol.TypeResponse, Error: &protocol.Error{Code: "auth", Message: "invalid token"}})
		return
	}
	s.tokenLimit.Reset(ip)
	welcome := protocol.Welcome{
		DaemonID:      s.daemonID,
		Version:       daemonVersion,
		OS:            runtime.GOOS,
		Arch:          runtime.GOARCH,
		RequireDocker: s.cfg.RequireDocker,
		DockerReady:   docker.Available(),
	}
	payload, _ := json.Marshal(welcome)
	if err := conn.WriteJSON(protocol.Message{Type: protocol.TypeResponse, Payload: payload}); err != nil {
		return
	}
	log.Printf("panel connected: remote=%s", r.RemoteAddr)

	sess := &session{conn: conn, subs: map[string]func(){}}
	// audit-2026-04-25 H2: per-session dispatch slot allocator. Capacity
	// from cfg.WSDispatchConcurrency (default 8192). Sized once per
	// connection so a runtime config reload affects future connections,
	// not the current one (avoids draining/refilling a live channel).
	dispatchSlots := s.cfg.WSDispatchConcurrency
	if dispatchSlots <= 0 {
		dispatchSlots = 8192
	}
	dispatchSem := make(chan struct{}, dispatchSlots)
	defer func() {
		sess.subMu.Lock()
		for _, un := range sess.subs {
			un()
		}
		sess.subMu.Unlock()
	}()

	conn.SetReadDeadline(time.Time{})
	conn.SetPongHandler(func(string) error { return nil })

	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			log.Printf("panel disconnected: %v", err)
			return
		}
		var msg protocol.Message
		if err := json.Unmarshal(raw, &msg); err != nil {
			continue
		}
		// audit-2026-04-25 H2: try to acquire a dispatch slot
		// non-blockingly. If the per-session budget is exhausted, the
		// panel must back off — reply with daemon.busy carrying the
		// original message ID so the in-flight call resolves with an
		// error instead of hanging forever.
		select {
		case dispatchSem <- struct{}{}:
			go func(m protocol.Message) {
				defer func() { <-dispatchSem }()
				s.dispatch(sess, m)
			}(msg)
		default:
			// Drop into the response only for request frames; events
			// have no caller waiting and busy-replying to them just
			// wastes bytes.
			if msg.Type == protocol.TypeRequest {
				_ = sess.writeJSON(protocol.Message{
					ID:   msg.ID,
					Type: protocol.TypeResponse,
					Error: &protocol.Error{
						Code:    "daemon.busy",
						Message: "daemon dispatch saturated; retry shortly",
					},
				})
			}
		}
	}
}

func (s *Server) dispatch(sess *session, msg protocol.Message) {
	if msg.Type != protocol.TypeRequest {
		return
	}
	resp := protocol.Message{ID: msg.ID, Type: protocol.TypeResponse}
	switch msg.Action {
	// ----- instance -----
	case protocol.ActionInstanceList:
		resp.Payload = mustMarshal(s.mgr.List())
	case protocol.ActionInstanceCreate:
		var cfg protocol.InstanceConfig
		if err := json.Unmarshal(msg.Payload, &cfg); err != nil {
			resp.Error = errOf("bad_request", err.Error())
			break
		}
		it, err := s.mgr.Create(cfg)
		if err != nil {
			resp.Error = errOf("create_failed", err.Error())
			break
		}
		resp.Payload = mustMarshal(it.Info())
	case protocol.ActionInstanceUpdate:
		var cfg protocol.InstanceConfig
		if err := json.Unmarshal(msg.Payload, &cfg); err != nil {
			resp.Error = errOf("bad_request", err.Error())
			break
		}
		it, err := s.mgr.Update(cfg)
		if err != nil {
			resp.Error = errOf("update_failed", err.Error())
			break
		}
		resp.Payload = mustMarshal(it.Info())
	case protocol.ActionInstanceStart:
		t, err := target(msg)
		if err != nil {
			resp.Error = errOf("bad_request", err.Error())
			break
		}
		it, ok := s.mgr.Get(t.UUID)
		if !ok {
			resp.Error = errOf("not_found", t.UUID)
			break
		}
		if err := it.Start(s.mgr.BaseDir()); err != nil {
			resp.Error = errOf("start_failed", err.Error())
			break
		}
		resp.Payload = mustMarshal(it.Info())
	case protocol.ActionInstanceStop:
		t, err := target(msg)
		if err != nil {
			resp.Error = errOf("bad_request", err.Error())
			break
		}
		it, ok := s.mgr.Get(t.UUID)
		if !ok {
			resp.Error = errOf("not_found", t.UUID)
			break
		}
		if err := it.Stop(); err != nil {
			resp.Error = errOf("stop_failed", err.Error())
		}
	case protocol.ActionInstanceKill:
		t, err := target(msg)
		if err != nil {
			resp.Error = errOf("bad_request", err.Error())
			break
		}
		it, ok := s.mgr.Get(t.UUID)
		if !ok {
			resp.Error = errOf("not_found", t.UUID)
			break
		}
		if err := it.Kill(); err != nil {
			resp.Error = errOf("kill_failed", err.Error())
		}
	case protocol.ActionInstanceDelete:
		t, err := target(msg)
		if err != nil {
			resp.Error = errOf("bad_request", err.Error())
			break
		}
		if err := s.mgr.Delete(t.UUID); err != nil {
			resp.Error = errOf("delete_failed", err.Error())
		}
	case protocol.ActionInstanceInput:
		var req protocol.InstanceInputReq
		if err := json.Unmarshal(msg.Payload, &req); err != nil {
			resp.Error = errOf("bad_request", err.Error())
			break
		}
		it, ok := s.mgr.Get(req.UUID)
		if !ok {
			resp.Error = errOf("not_found", req.UUID)
			break
		}
		if err := it.Input(req.Data); err != nil {
			resp.Error = errOf("input_failed", err.Error())
		}
	case protocol.ActionInstanceResize:
		var req protocol.InstanceResizeReq
		if err := json.Unmarshal(msg.Payload, &req); err != nil {
			resp.Error = errOf("bad_request", err.Error())
			break
		}
		it, ok := s.mgr.Get(req.UUID)
		if !ok {
			resp.Error = errOf("not_found", req.UUID)
			break
		}
		if err := it.Resize(req.Cols, req.Rows); err != nil {
			resp.Error = errOf("resize_failed", err.Error())
		}
	case protocol.ActionInstanceSubscribe:
		var req protocol.InstanceSubscribeReq
		if err := json.Unmarshal(msg.Payload, &req); err != nil {
			resp.Error = errOf("bad_request", err.Error())
			break
		}
		s.toggleSubscribe(sess, req)
	case protocol.ActionInstanceOutputHistory:
		var req protocol.InstanceTarget
		_ = json.Unmarshal(msg.Payload, &req)
		resp.Payload = mustMarshal(protocol.InstanceOutputHistoryResp{
			UUID:    req.UUID,
			History: s.mgr.Bus().OutputHistory(req.UUID),
		})
	case protocol.ActionInstanceDockerStats:
		var req protocol.InstanceTarget
		_ = json.Unmarshal(msg.Payload, &req)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		out := docker.Stats(ctx, "taps-"+req.UUID)
		cancel()
		// Augment with per-instance volume usage so user-facing dashboards
		// can show disk alongside CPU/mem in a single round-trip.
		if it, ok := s.mgr.Get(req.UUID); ok {
			cfg := it.Config()
			if cfg.ManagedVolume != "" && s.volumes != nil {
				vlist, _ := s.volumes.List()
				for _, v := range vlist.Volumes {
					if v.Name == cfg.ManagedVolume {
						out.DiskUsedBytes = v.UsedBytes
						out.DiskTotalBytes = v.SizeBytes
						break
					}
				}
			}
		}
		resp.Payload = mustMarshal(out)
	case protocol.ActionInstanceDockerStatsAll:
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		items := docker.StatsAll(ctx)
		cancel()
		// Index volumes once and stitch disk usage onto each container's
		// stats by matching the "taps-<uuid>" container name back to its
		// instance config.
		var vlist protocol.VolumeListResp
		if s.volumes != nil {
			vlist, _ = s.volumes.List()
		}
		volByName := map[string]protocol.Volume{}
		for _, v := range vlist.Volumes {
			volByName[v.Name] = v
		}
		for i := range items {
			if !strings.HasPrefix(items[i].Name, "taps-") {
				continue
			}
			uuid := items[i].Name[len("taps-"):]
			if it, ok := s.mgr.Get(uuid); ok {
				cfg := it.Config()
				if cfg.ManagedVolume != "" {
					if v, ok := volByName[cfg.ManagedVolume]; ok {
						items[i].DiskUsedBytes = v.UsedBytes
						items[i].DiskTotalBytes = v.SizeBytes
					}
				}
			}
		}
		resp.Payload = mustMarshal(protocol.DockerStatsAllResp{Items: items})

	// ----- fs -----
	case protocol.ActionFsList:
		var req protocol.FsListReq
		_ = json.Unmarshal(msg.Payload, &req)
		out, err := s.fs.List(req.Path)
		if err != nil {
			resp.Error = errOf("fs_list", err.Error())
			break
		}
		resp.Payload = mustMarshal(out)
	case protocol.ActionFsRead:
		var req protocol.FsReadReq
		_ = json.Unmarshal(msg.Payload, &req)
		out, err := s.fs.Read(req.Path)
		if err != nil {
			resp.Error = errOf("fs_read", err.Error())
			break
		}
		resp.Payload = mustMarshal(out)
	case protocol.ActionFsWrite:
		var req protocol.FsWriteReq
		_ = json.Unmarshal(msg.Payload, &req)
		if err := s.fs.Write(req.Path, req.Content); err != nil {
			resp.Error = errOf("fs_write", err.Error())
		}
	case protocol.ActionFsMkdir:
		var req protocol.FsPathReq
		_ = json.Unmarshal(msg.Payload, &req)
		if err := s.fs.Mkdir(req.Path); err != nil {
			resp.Error = errOf("fs_mkdir", err.Error())
		}
	case protocol.ActionFsDelete:
		var req protocol.FsPathReq
		_ = json.Unmarshal(msg.Payload, &req)
		if err := s.fs.Delete(req.Path); err != nil {
			resp.Error = errOf("fs_delete", err.Error())
		}
	case protocol.ActionFsRename:
		var req protocol.FsRenameReq
		_ = json.Unmarshal(msg.Payload, &req)
		if err := s.fs.Rename(req.From, req.To); err != nil {
			resp.Error = errOf("fs_rename", err.Error())
		}
	case protocol.ActionFsCopy:
		var req protocol.FsCopyReq
		_ = json.Unmarshal(msg.Payload, &req)
		if err := s.fs.Copy(req.From, req.To); err != nil {
			resp.Error = errOf("fs_copy", err.Error())
		}
	case protocol.ActionFsMove:
		var req protocol.FsMoveReq
		_ = json.Unmarshal(msg.Payload, &req)
		if err := s.fs.Move(req.From, req.To); err != nil {
			resp.Error = errOf("fs_move", err.Error())
		}
	case protocol.ActionFsZip:
		var req protocol.FsZipReq
		_ = json.Unmarshal(msg.Payload, &req)
		if err := s.fs.Zip(req.Paths, req.Dest); err != nil {
			resp.Error = errOf("fs_zip", err.Error())
		}
	case protocol.ActionFsUnzip:
		var req protocol.FsUnzipReq
		_ = json.Unmarshal(msg.Payload, &req)
		if err := s.fs.Unzip(req.Src, req.DestDir); err != nil {
			resp.Error = errOf("fs_unzip", err.Error())
		}

	// ----- monitor -----
	case protocol.ActionMonitorSnap:
		resp.Payload = mustMarshal(monitor.Snapshot())
	case protocol.ActionMonitorProcess:
		var req protocol.MonitorProcessReq
		_ = json.Unmarshal(msg.Payload, &req)
		it, ok := s.mgr.Get(req.UUID)
		if !ok {
			resp.Error = errOf("not_found", req.UUID)
			break
		}
		resp.Payload = mustMarshal(monitor.ProcessSnapshot(req.UUID, it.Info().PID))

	// ----- backups -----
	case protocol.ActionBackupList:
		var req protocol.BackupListReq
		_ = json.Unmarshal(msg.Payload, &req)
		ents, err := s.backup.List(req.UUID)
		if err != nil {
			resp.Error = errOf("backup_list", err.Error())
			break
		}
		resp.Payload = mustMarshal(protocol.BackupListResp{Entries: ents})
	case protocol.ActionBackupCreate:
		var req protocol.BackupCreateReq
		_ = json.Unmarshal(msg.Payload, &req)
		it, ok := s.mgr.Get(req.UUID)
		if !ok {
			resp.Error = errOf("not_found", req.UUID)
			break
		}
		ent, err := s.backup.Create(req.UUID, it.Config().WorkingDir, req.Note)
		if err != nil {
			resp.Error = errOf("backup_create", err.Error())
			break
		}
		resp.Payload = mustMarshal(ent)
	case protocol.ActionBackupRestore:
		var req protocol.BackupRestoreReq
		_ = json.Unmarshal(msg.Payload, &req)
		it, ok := s.mgr.Get(req.UUID)
		if !ok {
			resp.Error = errOf("not_found", req.UUID)
			break
		}
		if err := s.backup.Restore(req.UUID, it.Config().WorkingDir, req.Name); err != nil {
			resp.Error = errOf("backup_restore", err.Error())
		}
	case protocol.ActionBackupDelete:
		var req protocol.BackupDeleteReq
		_ = json.Unmarshal(msg.Payload, &req)
		if err := s.backup.Delete(req.UUID, req.Name); err != nil {
			resp.Error = errOf("backup_delete", err.Error())
		}

	// ----- docker -----
	case protocol.ActionDockerImages:
		out, _ := docker.List(context.Background())
		resp.Payload = mustMarshal(out)
	case protocol.ActionDockerPull:
		var req protocol.DockerPullReq
		_ = json.Unmarshal(msg.Payload, &req)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		var onLine func(string)
		if req.PullID != "" {
			pullID := req.PullID
			onLine = func(line string) {
				s.mgr.Bus().Publish(pullID, instance.EventDockerPull, line)
			}
		}
		err := docker.Pull(ctx, req.Image, onLine)
		if req.PullID != "" {
			errStr := ""
			if err != nil {
				errStr = err.Error()
			}
			s.mgr.Bus().Publish(req.PullID, instance.EventDockerPullDone, errStr)
		}
		if err != nil {
			resp.Error = errOf("docker_pull", err.Error())
		}
	case protocol.ActionDockerRemove:
		var req protocol.DockerRemoveReq
		_ = json.Unmarshal(msg.Payload, &req)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := docker.Remove(ctx, req.ID); err != nil {
			resp.Error = errOf("docker_remove", err.Error())
		}

	// ----- managed volumes -----
	case protocol.ActionVolumeList:
		out, _ := s.volumes.List()
		resp.Payload = mustMarshal(out)
	case protocol.ActionVolumeCreate:
		var req protocol.VolumeCreateReq
		_ = json.Unmarshal(msg.Payload, &req)
		v, err := s.volumes.Create(req)
		if err != nil {
			resp.Error = errOf("volume_create", err.Error())
			break
		}
		resp.Payload = mustMarshal(v)
	case protocol.ActionVolumeRemove:
		var req protocol.VolumeRemoveReq
		_ = json.Unmarshal(msg.Payload, &req)
		if err := s.volumes.Remove(req.Name); err != nil {
			resp.Error = errOf("volume_remove", err.Error())
		}
	case protocol.ActionVolumeDiskInfo:
		// Stat the partition that hosts the volumes directory; the panel's
		// group scheduler uses this to bias toward nodes with disk
		// headroom. Falls back to the daemon root when volumes is unset.
		root := ""
		if s.volumes != nil {
			root = s.volumes.Root()
		}
		if root == "" {
			root = "/"
		}
		var stat syscall.Statfs_t
		out := protocol.VolumeDiskInfoResp{}
		if err := syscall.Statfs(root, &stat); err == nil {
			out.TotalBytes = int64(stat.Blocks) * int64(stat.Bsize)
			out.FreeBytes = int64(stat.Bavail) * int64(stat.Bsize)
			out.UsedBytes = out.TotalBytes - out.FreeBytes
		}
		resp.Payload = mustMarshal(out)
	case protocol.ActionInstanceDeployStart:
		var req protocol.DeployStartReq
		if err := json.Unmarshal(msg.Payload, &req); err != nil {
			resp.Error = errOf("bad_request", err.Error())
			break
		}
		if s.deploy == nil {
			resp.Error = errOf("unavailable", "deploy manager not configured")
			break
		}
		if err := s.deploy.Start(req); err != nil {
			resp.Error = errOf("deploy_start", err.Error())
			break
		}
		resp.Payload = mustMarshal(protocol.DeployStartResp{Started: true})
	case protocol.ActionInstanceDeployStatus:
		var req protocol.InstanceTarget
		_ = json.Unmarshal(msg.Payload, &req)
		if s.deploy == nil {
			resp.Payload = mustMarshal(protocol.DeployStatus{UUID: req.UUID, Active: false})
			break
		}
		resp.Payload = mustMarshal(s.deploy.Status(req.UUID))

	// ----- minecraft -----
	case protocol.ActionMcPlayers:
		var req protocol.McPlayersReq
		_ = json.Unmarshal(msg.Payload, &req)
		it, ok := s.mgr.Get(req.UUID)
		if !ok {
			resp.Error = errOf("not_found", req.UUID)
			break
		}
		cfg := it.Config()
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		out, _ := minecraft.Ping(ctx, cfg.MinecraftHost, cfg.MinecraftPort)
		resp.Payload = mustMarshal(out)
	case protocol.ActionInstancePlayersAll:
		// Use the hibernation manager's cached snapshot — both the
		// dashboard and the watcher share one ping schedule.
		out := protocol.PlayersAllResp{Items: []protocol.PlayersBrief{}}
		if s.hib != nil {
			out.Items = s.hib.Players()
		}
		resp.Payload = mustMarshal(out)
	case protocol.ActionHibernationConfig:
		var hc protocol.HibernationConfig
		_ = json.Unmarshal(msg.Payload, &hc)
		if s.hib != nil {
			s.hib.ConfigUpdate(hc)
		}

	default:
		resp.Error = errOf("unknown_action", msg.Action)
	}
	_ = sess.writeJSON(resp)
}

func (s *Server) toggleSubscribe(sess *session, req protocol.InstanceSubscribeReq) {
	sess.subMu.Lock()
	defer sess.subMu.Unlock()
	if !req.Enabled {
		if un, ok := sess.subs[req.UUID]; ok {
			un()
			delete(sess.subs, req.UUID)
		}
		return
	}
	if _, ok := sess.subs[req.UUID]; ok {
		return
	}
	ch, un := s.mgr.Bus().Subscribe(req.UUID, 256)
	sess.subs[req.UUID] = un
	go func() {
		for ev := range ch {
			var action string
			var payload []byte
			switch ev.Kind {
			case instance.EventOutput:
				action = protocol.ActionInstanceOutput
				payload = mustMarshal(protocol.InstanceOutputEvent{UUID: ev.UUID, Data: ev.Data.(string)})
			case instance.EventStatus:
				action = protocol.ActionInstanceStatus
				payload = mustMarshal(map[string]any{"uuid": ev.UUID, "status": ev.Data})
			case instance.EventDockerPull:
				action = protocol.ActionDockerPullProgress
				payload = mustMarshal(protocol.DockerPullProgress{PullID: ev.UUID, Line: ev.Data.(string)})
			case instance.EventDockerPullDone:
				action = protocol.ActionDockerPullDone
				payload = mustMarshal(protocol.DockerPullDone{PullID: ev.UUID, Error: ev.Data.(string)})
			default:
				continue
			}
			_ = sess.writeJSON(protocol.Message{Type: protocol.TypeEvent, Action: action, Payload: payload})
		}
	}()
}

func target(msg protocol.Message) (protocol.InstanceTarget, error) {
	var t protocol.InstanceTarget
	err := json.Unmarshal(msg.Payload, &t)
	return t, err
}

func mustMarshal(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func errOf(code, msg string) *protocol.Error { return &protocol.Error{Code: code, Message: msg} }

// ----- HTTP file upload/download -----

// handleUploadInit declares an upcoming chunked upload. The panel
// sends total bytes / chunks / target path and gets back an uploadId
// that subsequent chunk requests must echo. Quota-checked against the
// volume that contains the target.
func (s *Server) handleUploadInit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "common.method_not_allowed", "method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	var req struct {
		Path        string `json:"path"`
		Filename    string `json:"filename"`
		TotalBytes  int64  `json:"totalBytes"`
		TotalChunks int    `json:"totalChunks"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "common.invalid_body", "invalid body: "+err.Error())
		return
	}
	if req.Path == "" {
		writeJSONError(w, http.StatusBadRequest, "fs.missing_path", "missing path")
		return
	}
	if req.TotalBytes <= 0 || req.TotalChunks <= 0 {
		writeJSONError(w, http.StatusBadRequest, "fs.invalid_chunk_dims", "totalBytes and totalChunks must be positive")
		return
	}
	abs, err := s.fs.Resolve(req.Path)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "fs.path_resolve_failed", err.Error())
		return
	}
	// Quota check: ask statfs for free space on the volume that hosts
	// `abs` (walks up to find an existing parent — the target file
	// usually doesn't exist yet). If volumes module is unavailable we
	// fall through (best-effort: better to allow than to refuse uploads
	// because the host is not using managed volumes).
	if free := freeBytesOf(abs); free > 0 {
		if req.TotalBytes > free {
			writeJSONErrorWithParams(w, http.StatusInsufficientStorage,
				"fs.quota_exceeded",
				"upload would exceed available storage on this volume",
				map[string]any{"freeBytes": free, "requested": req.TotalBytes})
			return
		}
	}
	sess, err := s.uploads.Init(abs, req.Filename, req.TotalBytes, req.TotalChunks)
	if err != nil {
		// audit-2026-04-25 MED6: surface the "same path already
		// uploading" branch with a stable error code + 409 so the
		// panel and SPA can recognise it (vs lumping it into the
		// generic upload_init_failed bucket).
		if errors.Is(err, uploadsession.ErrUploadInProgress) {
			writeJSONError(w, http.StatusConflict, "daemon.upload_in_progress",
				"another upload to this path is already in progress; wait for it to finish or cancel it")
			return
		}
		writeJSONError(w, http.StatusBadRequest, "fs.upload_init_failed", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"uploadId":  sess.ID,
		"expiresAt": time.Now().Add(uploadsession.DefaultTTL).Unix(),
	})
}

// freeBytesOf returns Bavail*Bsize for the closest existing ancestor
// of `path`. Returns 0 when statfs fails so the caller treats it as
// "unknown — don't enforce quota".
func freeBytesOf(path string) int64 {
	cur := path
	for cur != "" && cur != "/" {
		if _, err := os.Stat(cur); err == nil {
			return volumes.FreeBytesAt(cur)
		}
		next := filepath.Dir(cur)
		if next == cur {
			break
		}
		cur = next
	}
	return 0
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodPut {
		writeJSONError(w, http.StatusMethodNotAllowed, "common.method_not_allowed", "method not allowed")
		return
	}
	dest := r.URL.Query().Get("path")
	if dest == "" {
		writeJSONError(w, http.StatusBadRequest, "fs.missing_path", "missing path")
		return
	}
	abs, err := s.fs.Resolve(dest)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "fs.path_resolve_failed", err.Error())
		return
	}
	// limit body to 1 GiB per request (a single chunk for chunked mode, or whole file for single-shot)
	r.Body = http.MaxBytesReader(w, r.Body, 1<<30)

	ct := r.Header.Get("Content-Type")
	isMultipart := len(ct) >= len("multipart/") && ct[:len("multipart/")] == "multipart/"

	// Chunked mode: ?uploadId=&seq=N&total=M[&final=true]. Each call
	// appends to <abs>.partial. uploadId is required (hard cutover
	// per the Batch #6 design — no legacy fallback) and gates the
	// session against quota / total-bytes inflation.
	uploadID := r.URL.Query().Get("uploadId")
	seq := r.URL.Query().Get("seq")
	total := r.URL.Query().Get("total")
	final := r.URL.Query().Get("final") == "true"
	if seq != "" && total != "" {
		if uploadID == "" {
			writeJSONError(w, http.StatusBadRequest, "fs.upload_init_first",
				"missing uploadId — call POST /files/upload/init first")
			return
		}
		sess, ok := s.uploads.Get(uploadID)
		if !ok {
			writeJSONError(w, http.StatusGone, "fs.upload_unknown_id", "unknown or expired uploadId")
			return
		}
		if sess.PathAbs != abs {
			writeJSONError(w, http.StatusBadRequest, "fs.upload_path_mismatch",
				"path does not match init declaration")
			return
		}
		var src io.ReadCloser = r.Body
		if isMultipart {
			if err := r.ParseMultipartForm(64 << 20); err != nil {
				writeJSONError(w, http.StatusBadRequest, "fs.multipart_parse_failed", err.Error())
				return
			}
			f, _, err := r.FormFile("file")
			if err != nil {
				writeJSONError(w, http.StatusBadRequest, "fs.form_file_failed", err.Error())
				return
			}
			src = f
		}
		defer src.Close()
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "fs.mkdir_failed", err.Error())
			return
		}
		partial := abs + ".partial"
		flag := os.O_WRONLY | os.O_CREATE | os.O_APPEND
		if seq == "0" {
			flag = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
			// audit-2026-04-25-v2 MED16: truncating the .partial
			// without resetting the session counter would make the
			// inflation guard in Accept() add the new chunk's bytes
			// on top of the old session.received, immediately
			// tripping "chunk overruns declared totalBytes" on a
			// legitimate retry. Zero the counters so a real retry
			// starts from a clean slate.
			s.uploads.Reset(sess)
		}
		out, err := os.OpenFile(partial, flag, 0o644)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "fs.open_failed", err.Error())
			return
		}
		// Copy through a counter so we can record exactly what landed
		// on disk against the declared total.
		n, err := io.Copy(out, src)
		if err != nil {
			out.Close()
			os.Remove(partial)
			s.uploads.Cancel(uploadID)
			writeJSONError(w, http.StatusInternalServerError, "fs.write_failed", err.Error())
			return
		}
		if err := out.Close(); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "fs.close_failed", err.Error())
			return
		}
		if err := s.uploads.Accept(sess, n); err != nil {
			os.Remove(partial)
			s.uploads.Cancel(uploadID)
			writeJSONError(w, http.StatusBadRequest, "fs.upload_accept_failed", err.Error())
			return
		}
		if final {
			if sess.Received() != sess.TotalBytes {
				os.Remove(partial)
				s.uploads.Cancel(uploadID)
				writeJSONErrorWithParams(w, http.StatusBadRequest,
					"fs.final_chunk_short",
					fmt.Sprintf("final chunk but received %d / %d bytes", sess.Received(), sess.TotalBytes),
					map[string]any{"received": sess.Received(), "total": sess.TotalBytes})
				return
			}
			if err := os.Rename(partial, abs); err != nil {
				writeJSONError(w, http.StatusInternalServerError, "fs.rename_failed", err.Error())
				return
			}
			s.uploads.Finalize(sess)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
		return
	}

	// Single-shot mode (backward compat).
	if isMultipart {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			writeJSONError(w, http.StatusBadRequest, "fs.multipart_parse_failed", err.Error())
			return
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "fs.form_file_failed", err.Error())
			return
		}
		defer file.Close()
		if err := streamToFile(abs, file); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "fs.write_failed", err.Error())
			return
		}
	} else {
		if err := streamToFile(abs, r.Body); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "fs.write_failed", err.Error())
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	if p == "" {
		writeJSONError(w, http.StatusBadRequest, "fs.missing_path", "missing path")
		return
	}
	abs, err := s.fs.Resolve(p)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "fs.path_resolve_failed", err.Error())
		return
	}
	// Audit-2026-04-24-v3 M6: http.ServeFile generates an HTML
	// directory listing when given a directory path; that's an
	// information-disclosure vector for any caller who can guess
	// (or compose) a path resolving to one. Stat-and-reject up front.
	st, err := os.Stat(abs)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "fs.not_found", err.Error())
		return
	}
	if st.IsDir() {
		writeJSONError(w, http.StatusBadRequest, "fs.is_directory",
			"path is a directory; use the file-list endpoint to enumerate")
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="`+filepathBase(abs)+`"`)
	http.ServeFile(w, r, abs)
}

// handleBackupDownload streams one backup zip back to the panel. The
// backup module knows where the file lives (managed-volume override or
// the default backupsRoot) so we just ask it.
func (s *Server) handleBackupDownload(w http.ResponseWriter, r *http.Request) {
	uuid := r.URL.Query().Get("uuid")
	name := r.URL.Query().Get("name")
	if uuid == "" || name == "" {
		writeJSONError(w, http.StatusBadRequest, "fs.missing_uuid_name", "missing uuid/name")
		return
	}
	abs, err := s.backup.Path(uuid, name)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "fs.backup_path_failed", err.Error())
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="`+filepathBase(abs)+`"`)
	w.Header().Set("Content-Type", "application/zip")
	http.ServeFile(w, r, abs)
}

// mcAddressFor picks the (host, port) pair to ping for a given instance.
// Preference: explicit cfg.MinecraftHost/Port (set by the user when type=
// minecraft) → the host port from the first dockerPorts mapping (the one
// auto-allocated for docker MC instances). Returns port=0 when nothing
// usable is configured so the caller skips the ping.
func mcAddressFor(cfg protocol.InstanceConfig) (string, int) {
	host := cfg.MinecraftHost
	if host == "" {
		host = "127.0.0.1"
	}
	if cfg.MinecraftPort > 0 {
		return host, cfg.MinecraftPort
	}
	for _, spec := range cfg.DockerPorts {
		body := spec
		if i := strings.Index(body, "/"); i >= 0 {
			body = body[:i]
		}
		parts := strings.Split(body, ":")
		var hostStr string
		switch len(parts) {
		case 1:
			hostStr = parts[0]
		case 2:
			hostStr = parts[0]
		case 3:
			hostStr = parts[1]
		default:
			continue
		}
		if n, err := strconv.Atoi(strings.TrimSpace(hostStr)); err == nil && n > 0 {
			return host, n
		}
	}
	return host, 0
}
