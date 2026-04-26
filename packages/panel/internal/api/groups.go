package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/ProjectTapX/TapS/packages/panel/internal/auth"
	"github.com/ProjectTapX/TapS/packages/panel/internal/daemonclient"
	"github.com/ProjectTapX/TapS/packages/panel/internal/model"
	"github.com/ProjectTapX/TapS/packages/shared/protocol"
)

// GroupHandler manages NodeGroup records and the create-time scheduler
// that picks the best member when an admin uses a group instead of a
// specific daemon.
type GroupHandler struct {
	DB  *gorm.DB
	Reg *daemonclient.Registry
}

type groupView struct {
	ID        uint   `json:"id"`
	Name      string `json:"name"`
	DaemonIDs []uint `json:"daemonIds"`
}

func (h *GroupHandler) loadAll() ([]groupView, error) {
	var gs []model.NodeGroup
	if err := h.DB.Order("id").Find(&gs).Error; err != nil {
		return nil, err
	}
	var members []model.NodeGroupMember
	if err := h.DB.Find(&members).Error; err != nil {
		return nil, err
	}
	byGroup := map[uint][]uint{}
	for _, m := range members {
		byGroup[m.GroupID] = append(byGroup[m.GroupID], m.DaemonID)
	}
	out := make([]groupView, 0, len(gs))
	for _, g := range gs {
		out = append(out, groupView{ID: g.ID, Name: g.Name, DaemonIDs: byGroup[g.ID]})
	}
	return out, nil
}

func (h *GroupHandler) List(c *gin.Context) {
	out, err := h.loadAll()
	if err != nil {
		apiErr(c, http.StatusInternalServerError, "common.internal", err.Error())
		return
	}
	c.JSON(http.StatusOK, out)
}

type groupBody struct {
	Name      string `json:"name"`
	DaemonIDs []uint `json:"daemonIds"`
}

func (h *GroupHandler) Create(c *gin.Context) {
	var b groupBody
	if err := c.ShouldBindJSON(&b); err != nil {
		apiErr(c, http.StatusBadRequest, "common.invalid_body", "invalid body")
		return
	}
	name := strings.TrimSpace(b.Name)
	if name == "" || len(name) > 64 {
		apiErr(c, http.StatusBadRequest, "common.name_required", "name required (1-64 chars)")
		return
	}
	g := model.NodeGroup{Name: name, CreatedAt: time.Now()}
	if err := h.DB.Create(&g).Error; err != nil {
		apiErr(c, http.StatusBadRequest, "common.bad_request", err.Error())
		return
	}
	if err := h.replaceMembers(g.ID, b.DaemonIDs); err != nil {
		apiErr(c, http.StatusInternalServerError, "common.internal", err.Error())
		return
	}
	c.JSON(http.StatusOK, groupView{ID: g.ID, Name: g.Name, DaemonIDs: b.DaemonIDs})
}

func (h *GroupHandler) Update(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var b groupBody
	if err := c.ShouldBindJSON(&b); err != nil {
		apiErr(c, http.StatusBadRequest, "common.invalid_body", "invalid body")
		return
	}
	var g model.NodeGroup
	if err := h.DB.First(&g, id).Error; err != nil {
		apiErr(c, http.StatusNotFound, "common.group_not_found", "group not found")
		return
	}
	name := strings.TrimSpace(b.Name)
	if name == "" || len(name) > 64 {
		apiErr(c, http.StatusBadRequest, "common.name_required", "name required (1-64 chars)")
		return
	}
	g.Name = name
	if err := h.DB.Save(&g).Error; err != nil {
		apiErr(c, http.StatusBadRequest, "common.bad_request", err.Error())
		return
	}
	if err := h.replaceMembers(g.ID, b.DaemonIDs); err != nil {
		apiErr(c, http.StatusInternalServerError, "common.internal", err.Error())
		return
	}
	c.JSON(http.StatusOK, groupView{ID: g.ID, Name: g.Name, DaemonIDs: b.DaemonIDs})
}

func (h *GroupHandler) Delete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("group_id = ?", id).Delete(&model.NodeGroupMember{}).Error; err != nil {
			return err
		}
		return tx.Delete(&model.NodeGroup{}, id).Error
	})
	if err != nil {
		apiErr(c, http.StatusInternalServerError, "common.internal", err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// replaceMembers atomically swaps the membership list for one group.
func (h *GroupHandler) replaceMembers(groupID uint, daemonIDs []uint) error {
	return h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("group_id = ?", groupID).Delete(&model.NodeGroupMember{}).Error; err != nil {
			return err
		}
		seen := map[uint]bool{}
		rows := make([]model.NodeGroupMember, 0, len(daemonIDs))
		for _, did := range daemonIDs {
			if did == 0 || seen[did] {
				continue
			}
			seen[did] = true
			rows = append(rows, model.NodeGroupMember{GroupID: groupID, DaemonID: did})
		}
		if len(rows) == 0 {
			return nil
		}
		return tx.Create(&rows).Error
	})
}

// ----- resolve -----

type resolveReq struct {
	Type string `json:"type"` // "docker" or other
	Port int    `json:"port"` // 0 = auto-pick after node selected
}

type resolveResp struct {
	DaemonID     uint   `json:"daemonId"`
	DaemonName   string `json:"daemonName"`
	Port         int    `json:"port"`
	PortFree     bool   `json:"portFree"`
	FallbackUsed bool   `json:"fallbackUsed"`
	Warning      string `json:"warning,omitempty"`
}

// candidateMetric is one daemon's current load + disk snapshot.
type candidateMetric struct {
	id          uint
	name        string
	memPercent  float64 // 0..100
	diskFreePct float64 // 0..100
	usedPorts   map[int]bool
	portMin     int
	portMax     int
	ok          bool
}

// pickFromGroup runs the scheduler. Returns the chosen daemon, whether
// the disk fallback path was taken, and a non-fatal warning string.
// Returns an error only when there are zero usable candidates.
func (h *GroupHandler) pickFromGroup(groupID uint, dockerType bool) (*candidateMetric, bool, string, error) {
	var members []model.NodeGroupMember
	if err := h.DB.Where("group_id = ?", groupID).Find(&members).Error; err != nil {
		return nil, false, "", err
	}
	if len(members) == 0 {
		return nil, false, "", errors.New("group has no members")
	}

	// Probe each member's load + disk concurrently with a 5s budget so
	// one slow daemon can't stall the whole resolve.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	var mu sync.Mutex
	cands := make([]candidateMetric, 0, len(members))

	for _, mb := range members {
		cli, ok := h.Reg.Get(mb.DaemonID)
		if !ok || !cli.Connected() {
			continue
		}
		// Filter docker-only mismatches up front.
		if dockerType {
			w := cli.Welcome()
			if !w.DockerReady {
				continue
			}
		}
		var d model.Daemon
		_ = h.DB.First(&d, mb.DaemonID).Error
		wg.Add(1)
		go func(d model.Daemon, cli *daemonclient.Client) {
			defer wg.Done()
			cm := candidateMetric{id: d.ID, name: d.Name, ok: false}
			cm.portMin = d.PortMin
			cm.portMax = d.PortMax
			if cm.portMin <= 0 {
				cm.portMin = 25565
			}
			if cm.portMax <= 0 || cm.portMax < cm.portMin {
				cm.portMax = 25600
			}

			// monitor snapshot for memory pressure
			memCtx, mc := context.WithTimeout(ctx, 4*time.Second)
			defer mc()
			if raw, err := cli.Call(memCtx, protocol.ActionMonitorSnap, struct{}{}); err == nil {
				var s struct {
					MemPercent float64 `json:"memPercent"`
				}
				_ = json.Unmarshal(raw, &s)
				cm.memPercent = s.MemPercent
			}
			// volume-partition disk info
			if raw, err := cli.Call(memCtx, protocol.ActionVolumeDiskInfo, struct{}{}); err == nil {
				var di protocol.VolumeDiskInfoResp
				_ = json.Unmarshal(raw, &di)
				if di.TotalBytes > 0 {
					cm.diskFreePct = float64(di.FreeBytes) / float64(di.TotalBytes) * 100
				}
			}
			// instance list → used host ports (used by port-collision check)
			if raw, err := cli.Call(memCtx, protocol.ActionInstanceList, struct{}{}); err == nil {
				var infos []protocol.InstanceInfo
				_ = json.Unmarshal(raw, &infos)
				cm.usedPorts = map[int]bool{}
				for _, info := range infos {
					for _, p := range info.Config.DockerPorts {
						if hp := parseHostPort(p); hp > 0 {
							cm.usedPorts[hp] = true
						}
					}
				}
				cm.ok = true
			}
			mu.Lock()
			cands = append(cands, cm)
			mu.Unlock()
		}(d, cli)
	}
	wg.Wait()

	// Drop any that didn't even get an instance list back.
	usable := cands[:0]
	for _, c := range cands {
		if c.ok {
			usable = append(usable, c)
		}
	}
	if len(usable) == 0 {
		return nil, false, "", errors.New("no usable members in group (none connected / docker-ready)")
	}

	// Primary path: filter to disk_free% >= 20, pick lowest mem.
	const diskThreshold = 20.0
	primary := make([]candidateMetric, 0, len(usable))
	for _, c := range usable {
		if c.diskFreePct >= diskThreshold {
			primary = append(primary, c)
		}
	}
	if len(primary) > 0 {
		best := primary[0]
		for _, c := range primary[1:] {
			if c.memPercent < best.memPercent {
				best = c
			}
		}
		return &best, false, "", nil
	}
	// Fallback: nobody has 20% free — pick the most spacious. Caller is
	// expected to surface the warning.
	best := usable[0]
	for _, c := range usable[1:] {
		if c.diskFreePct > best.diskFreePct {
			best = c
		}
	}
	return &best, true, "no member has ≥20% free disk; picked the most spacious", nil
}

// pickFreePort returns the smallest free port within the daemon's range,
// preferring `prefer` if it's free and in-range. Returns 0 if no port is
// available anywhere in the range.
func pickFreePort(used map[int]bool, lo, hi, prefer int) int {
	if prefer >= lo && prefer <= hi && !used[prefer] {
		return prefer
	}
	for n := lo; n <= hi; n++ {
		if !used[n] {
			return n
		}
	}
	return 0
}

func (h *GroupHandler) Resolve(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var req resolveReq
	_ = c.ShouldBindJSON(&req)

	dockerType := req.Type == "" || req.Type == "docker"
	chosen, fallback, warn, err := h.pickFromGroup(uint(id), dockerType)
	if err != nil {
		apiErr(c, http.StatusBadRequest, "common.bad_request", err.Error())
		return
	}

	out := resolveResp{
		DaemonID:     chosen.id,
		DaemonName:   chosen.name,
		FallbackUsed: fallback,
		Warning:      warn,
	}

	if req.Port > 0 {
		// User typed a port. Report whether it's free on the chosen
		// node. Out-of-range counts as not free with an explanation.
		if req.Port < chosen.portMin || req.Port > chosen.portMax {
			out.Port = req.Port
			out.PortFree = false
			if out.Warning == "" {
				out.Warning = "port outside this node's allowed range"
			}
		} else {
			out.Port = req.Port
			out.PortFree = !chosen.usedPorts[req.Port]
		}
	} else {
		// Auto-pick: smallest free port in range.
		port := pickFreePort(chosen.usedPorts, chosen.portMin, chosen.portMax, chosen.portMin)
		if port == 0 {
			out.PortFree = false
			if out.Warning == "" {
				out.Warning = "no free port in this node's range"
			}
		} else {
			out.Port = port
			out.PortFree = true
		}
	}

	c.JSON(http.StatusOK, out)
}

// CreateInstance receives the same body shape as the per-daemon
// instance.create, but routes it via the group scheduler instead of a
// fixed daemon. Re-runs the resolver authoritatively (the form's
// resolve calls are advisory — node load shifts between picks). If the
// caller supplied a host port spec we keep it; otherwise we synthesize
// "<port>:<containerPort>" from the resolver's chosen port.
func (h *GroupHandler) CreateInstance(c *gin.Context) {
	role, _ := c.Get(auth.CtxRole)
	if role != model.RoleAdmin {
		apiErr(c, http.StatusForbidden, "auth.forbidden", "admin only")
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))

	raw, err := c.GetRawData()
	if err != nil {
		apiErr(c, http.StatusBadRequest, "common.invalid_body", "invalid body")
		return
	}
	var cfg protocol.InstanceConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		apiErr(c, http.StatusBadRequest, "common.invalid_body", "invalid body")
		return
	}
	var extras struct {
		HostPort      int `json:"hostPort"`
		ContainerPort int `json:"containerPort"`
	}
	_ = json.Unmarshal(raw, &extras)

	dockerType := cfg.Type == "" || cfg.Type == "docker"
	chosen, fallback, warn, err := h.pickFromGroup(uint(id), dockerType)
	if err != nil {
		apiErr(c, http.StatusBadRequest, "common.bad_request", err.Error())
		return
	}
	_ = fallback

	port := extras.HostPort
	if port > 0 {
		if port < chosen.portMin || port > chosen.portMax {
			apiErr(c, http.StatusBadRequest, "daemon.port_out_of_range", "port outside chosen node's allowed range: " + chosen.name)
			return
		}
		if chosen.usedPorts[port] {
			apiErr(c, http.StatusConflict, "daemon.port_in_use", "port already in use on chosen node: " + chosen.name)
			return
		}
	} else {
		port = pickFreePort(chosen.usedPorts, chosen.portMin, chosen.portMax, chosen.portMin)
		if port == 0 {
			apiErr(c, http.StatusConflict, "daemon.no_free_port", "no free port in chosen node's range: " + chosen.name)
			return
		}
	}
	if dockerType {
		container := extras.ContainerPort
		if container <= 0 {
			container = port
		}
		cfg.DockerPorts = []string{strconv.Itoa(port) + ":" + strconv.Itoa(container)}
	}
	if strings.TrimSpace(cfg.Name) == "" {
		cfg.Name = autoInstanceName(cfg)
	}

	cli, ok := h.Reg.Get(chosen.id)
	if !ok || !cli.Connected() {
		apiErr(c, http.StatusBadGateway, "daemon.offline_retry", "chosen node went offline; please retry")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	out, err := cli.Call(ctx, protocol.ActionInstanceCreate, cfg)
	if err != nil {
		apiErr(c, http.StatusBadGateway, "common.internal", err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"daemonId":   chosen.id,
		"daemonName": chosen.name,
		"warning":    warn,
		"info":       json.RawMessage(out),
	})
}
