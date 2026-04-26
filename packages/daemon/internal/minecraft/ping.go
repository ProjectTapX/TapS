// Package minecraft implements the Minecraft Server List Ping protocol so the
// daemon can report a server's online players without parsing console output.
// See https://wiki.vg/Server_List_Ping (post-1.7 modern handshake).
package minecraft

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/taps/shared/protocol"
)

// Ping connects to a 1.7+ server and returns its status line.
func Ping(ctx context.Context, host string, port int) (protocol.McPlayersResp, error) {
	if host == "" {
		host = "127.0.0.1"
	}
	if port == 0 {
		port = 25565
	}
	addr := fmt.Sprintf("%s:%d", host, port)
	d := net.Dialer{Timeout: 3 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return protocol.McPlayersResp{Online: false, Error: err.Error()}, nil
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))

	// Handshake (next state = 1, "status")
	hs := buildHandshake(host, uint16(port))
	if err := writePacket(conn, 0x00, hs); err != nil {
		return protocol.McPlayersResp{Online: false, Error: err.Error()}, nil
	}
	// Status request (empty payload)
	if err := writePacket(conn, 0x00, nil); err != nil {
		return protocol.McPlayersResp{Online: false, Error: err.Error()}, nil
	}

	r := bufio.NewReader(conn)
	if _, err := readVarInt(r); err != nil { // total packet length
		return protocol.McPlayersResp{Online: false, Error: err.Error()}, nil
	}
	pid, err := readVarInt(r)
	if err != nil || pid != 0x00 {
		return protocol.McPlayersResp{Online: false, Error: "bad response"}, nil
	}
	jsonLen, err := readVarInt(r)
	if err != nil {
		return protocol.McPlayersResp{Online: false, Error: err.Error()}, nil
	}
	body := make([]byte, jsonLen)
	if _, err := io.ReadFull(r, body); err != nil {
		return protocol.McPlayersResp{Online: false, Error: err.Error()}, nil
	}

	var raw struct {
		Version     struct{ Name string } `json:"version"`
		Players     struct {
			Max    int `json:"max"`
			Online int `json:"online"`
			Sample []struct {
				Name string `json:"name"`
				ID   string `json:"id"`
			} `json:"sample"`
		} `json:"players"`
		Description any `json:"description"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return protocol.McPlayersResp{Online: false, Error: err.Error()}, nil
	}
	out := protocol.McPlayersResp{
		Online:  true,
		Version: raw.Version.Name,
		Max:     raw.Players.Max,
		Count:   raw.Players.Online,
	}
	for _, p := range raw.Players.Sample {
		out.Players = append(out.Players, protocol.McPlayer{Name: p.Name, UUID: p.ID})
	}
	out.Description = describeMOTD(raw.Description)
	return out, nil
}

// describeMOTD reduces the various MOTD shapes (string or chat component) to
// a flat user-facing string.
func describeMOTD(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case map[string]any:
		s := ""
		if x, ok := t["text"].(string); ok {
			s += x
		}
		if extra, ok := t["extra"].([]any); ok {
			for _, e := range extra {
				s += describeMOTD(e)
			}
		}
		return s
	}
	return ""
}

// ----- low-level packet/varint helpers -----

func writePacket(w io.Writer, id byte, payload []byte) error {
	body := append([]byte{id}, payload...)
	hdr := encodeVarInt(int32(len(body)))
	_, err := w.Write(append(hdr, body...))
	return err
}

func buildHandshake(host string, port uint16) []byte {
	buf := make([]byte, 0, 16)
	buf = append(buf, encodeVarInt(-1)...) // protocol version: -1 = "any"; many servers also accept 47
	buf = append(buf, encodeVarInt(int32(len(host)))...)
	buf = append(buf, []byte(host)...)
	p := make([]byte, 2)
	binary.BigEndian.PutUint16(p, port)
	buf = append(buf, p...)
	buf = append(buf, encodeVarInt(1)...) // next state: status
	return buf
}

func encodeVarInt(v int32) []byte {
	u := uint32(v)
	out := make([]byte, 0, 5)
	for {
		b := byte(u & 0x7f)
		u >>= 7
		if u != 0 {
			b |= 0x80
		}
		out = append(out, b)
		if u == 0 {
			return out
		}
	}
}

func readVarInt(r io.ByteReader) (int32, error) {
	var (
		v     uint32
		shift uint
	)
	for i := 0; i < 5; i++ {
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		v |= uint32(b&0x7f) << shift
		if b&0x80 == 0 {
			return int32(v), nil
		}
		shift += 7
	}
	return 0, errors.New("varint too long")
}
