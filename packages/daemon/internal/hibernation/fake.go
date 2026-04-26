package hibernation

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ProjectTapX/TapS/packages/shared/protocol"
)

// fakeServer impersonates a Minecraft server so that:
//   - clients querying the multiplayer list see our hibernation MOTD;
//   - clients trying to actually join get a localized kick message and
//     the real container is started in the background.
//
// We only need to implement the modern post-1.7 handshake. Old (pre-1.7)
// "legacy ping" clients (≤ MC 1.6) will see a connection close — that's
// fine; nobody runs those any more.
type fakeServer struct {
	listener net.Listener
	uuid     string
	wakeOnce sync.Once
	onWake   func(uuid string)

	cfgMu sync.RWMutex
	cfg   protocol.HibernationConfig

	closed atomic.Bool
}

func newFakeServer(addr string, cfg protocol.HibernationConfig, uuid string, onWake func(string)) (*fakeServer, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	fs := &fakeServer{
		listener: ln,
		uuid:     uuid,
		cfg:      cfg,
		onWake:   onWake,
	}
	go fs.accept()
	return fs, nil
}

func (f *fakeServer) Close() error {
	if f.closed.Swap(true) {
		return nil
	}
	return f.listener.Close()
}

func (f *fakeServer) UpdateConfig(cfg protocol.HibernationConfig) {
	f.cfgMu.Lock()
	f.cfg = cfg
	f.cfgMu.Unlock()
}

func (f *fakeServer) accept() {
	for {
		conn, err := f.listener.Accept()
		if err != nil {
			if f.closed.Load() {
				return
			}
			// Transient accept failures (file descriptor pressure, etc.)
			// shouldn't kill the whole listener — back off and retry.
			log.Printf("hibernation fake[%s]: accept: %v", f.uuid, err)
			time.Sleep(time.Second)
			continue
		}
		go f.handle(conn)
	}
}

func (f *fakeServer) handle(conn net.Conn) {
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	br := bufio.NewReader(conn)
	// Modern handshake: VarInt length + VarInt packet ID 0x00 + payload.
	// Legacy 1.6 ping starts with 0xFE; we just close on those.
	first, err := br.Peek(1)
	if err != nil || first[0] == 0xFE {
		return
	}
	pktLen, err := readVarInt(br)
	if err != nil || pktLen <= 0 || pktLen > 256 {
		return
	}
	body := make([]byte, pktLen)
	if _, err := io.ReadFull(br, body); err != nil {
		return
	}
	pktID, n := decodeVarInt(body)
	if pktID != 0 {
		return
	}
	body = body[n:]
	// Handshake payload: protoVersion VarInt, serverAddr String, port u16, nextState VarInt
	_, n = decodeVarInt(body) // proto
	body = body[n:]
	addrLen, n := decodeVarInt(body)
	body = body[n:]
	if int(addrLen) > len(body) {
		return
	}
	body = body[addrLen+2:] // skip addr + port
	nextState, _ := decodeVarInt(body)

	switch nextState {
	case 1:
		f.handleStatus(conn, br)
	case 2:
		f.handleLogin(conn, br)
	}
}

func (f *fakeServer) handleStatus(conn net.Conn, br *bufio.Reader) {
	// Expect status request (empty packet) then ping request (8-byte payload).
	pktLen, err := readVarInt(br)
	if err != nil || pktLen <= 0 {
		return
	}
	body := make([]byte, pktLen)
	if _, err := io.ReadFull(br, body); err != nil {
		return
	}
	pktID, _ := decodeVarInt(body)
	if pktID != 0 {
		return
	}
	// Build the JSON status response.
	f.cfgMu.RLock()
	cfg := f.cfg
	f.cfgMu.RUnlock()
	resp := map[string]any{
		"version": map[string]any{"name": "Hibernating", "protocol": 0},
		"players": map[string]any{"max": 0, "online": 0, "sample": []any{}},
		"description": map[string]any{"text": cfg.MOTD},
	}
	if len(cfg.IconPNG) > 0 {
		resp["favicon"] = "data:image/png;base64," + base64.StdEncoding.EncodeToString(cfg.IconPNG)
	}
	jsonBytes, _ := json.Marshal(resp)
	// Outgoing packet: VarInt(len(packet)) + packet
	// packet = VarInt(0x00) + VarInt(len(json)) + jsonBytes
	pkt := encodePacket(0, encodeString(string(jsonBytes)))
	if _, err := conn.Write(pkt); err != nil {
		return
	}
	// Optional ping/pong — echo back the 8 bytes so the client shows latency.
	pktLen, err = readVarInt(br)
	if err != nil || pktLen != 9 { // 1 byte id + 8 byte payload
		return
	}
	pingBody := make([]byte, pktLen)
	if _, err := io.ReadFull(br, pingBody); err != nil {
		return
	}
	pid, n := decodeVarInt(pingBody)
	if pid != 1 || len(pingBody)-n != 8 {
		return
	}
	pong := encodePacket(1, pingBody[n:])
	_, _ = conn.Write(pong)
}

func (f *fakeServer) handleLogin(conn net.Conn, br *bufio.Reader) {
	// Read the login start packet (we don't actually need it, but draining
	// avoids RST that could confuse some clients).
	pktLen, err := readVarInt(br)
	if err == nil && pktLen > 0 && pktLen < 4096 {
		_, _ = io.CopyN(io.Discard, br, int64(pktLen))
	}
	f.cfgMu.RLock()
	kick := f.cfg.KickMessage
	f.cfgMu.RUnlock()
	// Login Disconnect: packet 0x00 with a JSON chat component.
	chat, _ := json.Marshal(map[string]any{"text": kick})
	pkt := encodePacket(0, encodeString(string(chat)))
	_, _ = conn.Write(pkt)
	// Trigger wake exactly once per fake-server lifetime — multiple
	// rapid connects shouldn't cause multiple Start calls.
	f.wakeOnce.Do(func() {
		go f.onWake(f.uuid)
	})
}

// ----- VarInt / String helpers (Minecraft wire format) -----

func readVarInt(r *bufio.Reader) (int32, error) {
	var v int32
	var shift uint
	for i := 0; i < 5; i++ {
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		v |= int32(b&0x7F) << shift
		if b&0x80 == 0 {
			return v, nil
		}
		shift += 7
	}
	return 0, errors.New("varint too long")
}
func decodeVarInt(b []byte) (int32, int) {
	var v int32
	var shift uint
	for i := 0; i < len(b) && i < 5; i++ {
		v |= int32(b[i]&0x7F) << shift
		if b[i]&0x80 == 0 {
			return v, i + 1
		}
		shift += 7
	}
	return 0, 0
}
func encodeVarInt(v int32) []byte {
	var out []byte
	uv := uint32(v)
	for {
		if uv&^0x7F == 0 {
			out = append(out, byte(uv))
			return out
		}
		out = append(out, byte(uv&0x7F)|0x80)
		uv >>= 7
	}
}
func encodeString(s string) []byte {
	var buf bytes.Buffer
	buf.Write(encodeVarInt(int32(len(s))))
	buf.WriteString(s)
	return buf.Bytes()
}
func encodePacket(id int32, payload []byte) []byte {
	body := append(encodeVarInt(id), payload...)
	return append(encodeVarInt(int32(len(body))), body...)
}

// silence unused-import lint; binary stays imported in case we add more
// fixed-size fields later.
var _ = binary.BigEndian
