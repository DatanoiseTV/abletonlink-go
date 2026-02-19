package main

import (
	"bytes"
	"embed"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	abletonlink "github.com/DatanoiseTV/abletonlink-go"
	"github.com/braheezy/shine-mp3/pkg/mp3"
	"github.com/gorilla/websocket"
)

//go:embed index.html
var content embed.FS

const (
	SampleRate = 48000
	FrameSize  = 1152
	Channels   = 2
)

// -- Resampler --

type Resampler struct {
	ratio    float64
	channels int
	position float64
	pending  []int16
	inRate   int
}

func NewResampler(inRate, outRate, channels int) *Resampler {
	return &Resampler{
		ratio:    float64(inRate) / float64(outRate),
		channels: channels,
		pending:  make([]int16, 0, 4096),
		inRate:   inRate,
	}
}

func (r *Resampler) Resample(input []int16) []int16 {
	if r.ratio == 1.0 { return input }
	r.pending = append(r.pending, input...)
	inFrames := len(r.pending) / r.channels
	if inFrames < 2 { return nil }
	numOutFrames := int((float64(inFrames) - 1 - r.position) / r.ratio)
	if numOutFrames <= 0 { return nil }
	output := make([]int16, numOutFrames*r.channels)
	for i := 0; i < numOutFrames; i++ {
		inPos := r.position + float64(i)*r.ratio
		idx := int(inPos)
		frac := inPos - float64(idx)
		for c := 0; c < r.channels; c++ {
			v1 := float64(r.pending[idx*r.channels+c])
			v2 := float64(r.pending[(idx+1)*r.channels+c])
			output[i*r.channels+c] = int16(v1 + frac*(v2-v1))
		}
	}
	consumedInFrames := int(r.position + float64(numOutFrames)*r.ratio)
	r.position = (r.position + float64(numOutFrames)*r.ratio) - float64(consumedInFrames)
	remainingSamples := len(r.pending) - consumedInFrames*r.channels
	copy(r.pending, r.pending[consumedInFrames*r.channels:])
	r.pending = r.pending[:remainingSamples]
	return output
}

// -- Protocol --

type WSMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

type MixerState struct {
	BPM       float64        `json:"bpm"`
	Playing   bool           `json:"playing"`
	Streaming bool           `json:"streaming"`
	Peers     uint64         `json:"peers"`
	Master    ChannelState   `json:"master"`
	Channels  []ChannelState `json:"channels"`
}

type ChannelState struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	PeerName string  `json:"peer_name"`
	Volume   float64 `json:"volume"`
	Muted    bool    `json:"muted"`
	Soloed   bool    `json:"soloed"`
}

type VolumeCmd struct {
	ID    string  `json:"id"`
	Value float64 `json:"value"`
}

type BoolCmd struct {
	ID    string `json:"id"`
	Value bool   `json:"value"`
}

type TransportCmd struct {
	Playing bool `json:"playing"`
}

type MonitorCmd struct {
	Enabled bool `json:"enabled"`
}

type StreamConfig struct {
	Host  string `json:"host"`
	Port  int    `json:"port"`
	User  string `json:"user"`
	Pass  string `json:"pass"`
	Mount string `json:"mount"`
}

// -- Audio Engine --

type ChannelStrip struct {
	ID       string
	Name     string
	PeerName string
	buffer   []int16
	mu       sync.Mutex
	Volume   float64
	Muted    bool
	Soloed   bool
	PeakHold float64
	source   *abletonlink.Source
	resamp   *Resampler
}

func (cs *ChannelStrip) Push(samples []int16) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if len(cs.buffer) > SampleRate*Channels { cs.buffer = cs.buffer[len(cs.buffer)/2:] }
	cs.buffer = append(cs.buffer, samples...)
}

func (cs *ChannelStrip) Pop(count int) []int16 {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if len(cs.buffer) < count {
		out := make([]int16, count)
		copy(out, cs.buffer)
		cs.buffer = cs.buffer[:0]
		return out
	}
	out := make([]int16, count)
	copy(out, cs.buffer[:count])
	cs.buffer = cs.buffer[count:]
	return out
}

type Mixer struct {
	MasterVolume float64
	MasterMuted  bool
	MasterPeak   float64
	Channels     map[string]*ChannelStrip
	mu           sync.RWMutex
	Link         *abletonlink.Link
	streaming    bool
	streamConn   net.Conn
	streamEnc    *mp3.Encoder
	streamMu     sync.Mutex
}

func (m *Mixer) StartStream(cfg StreamConfig) error {
	m.streamMu.Lock()
	defer m.streamMu.Unlock()
	if m.streaming { return fmt.Errorf("already streaming") }
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil { return err }
	auth := base64.StdEncoding.EncodeToString([]byte(cfg.User + ":" + cfg.Pass))
	header := fmt.Sprintf("SOURCE /%s HTTP/1.0\r\nAuthorization: Basic %s\r\nContent-Type: audio/mpeg\r\nIce-Name: Link Mixer\r\n\r\n", strings.TrimPrefix(cfg.Mount, "/"), auth)
	conn.Write([]byte(header))
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp := make([]byte, 1024)
	n, err := conn.Read(resp)
	if err != nil || !strings.Contains(string(resp[:n]), "200") {
		conn.Close()
		return fmt.Errorf("rejected: %s", string(resp[:n]))
	}
	conn.SetReadDeadline(time.Time{})
	m.streamConn = conn
	m.streamEnc = mp3.NewEncoder(SampleRate, Channels)
	m.streaming = true
	return nil
}

func (m *Mixer) StopStream() {
	m.streamMu.Lock()
	defer m.streamMu.Unlock()
	if m.streamConn != nil { m.streamConn.Close(); m.streamConn = nil }
	m.streaming = false
}

func (m *Mixer) Process(frameCount int) []int16 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	mixL, mixR := make([]float64, frameCount), make([]float64, frameCount)
	anySolo := false
	for _, ch := range m.Channels { if ch.Soloed { anySolo = true; break } }
	for _, ch := range m.Channels {
		input := ch.Pop(frameCount * 2)
		var peak float64
		for _, s := range input {
			abs := math.Abs(float64(s))
			if abs > peak { peak = abs }
		}
		p := peak / 32768.0
		if p > ch.PeakHold { ch.PeakHold = p }
		if ch.Muted || (anySolo && !ch.Soloed) { continue }
		for i := 0; i < frameCount; i++ {
			mixL[i] += float64(input[i*2]) * ch.Volume
			mixR[i] += float64(input[i*2+1]) * ch.Volume
		}
	}
	out := make([]int16, frameCount*2)
	mVol := m.MasterVolume
	if m.MasterMuted { mVol = 0 }
	var mp float64
	for i := 0; i < frameCount; i++ {
		l, r := mixL[i]*mVol, mixR[i]*mVol
		if l > 32767 { l = 32767 } else if l < -32768 { l = -32768 }
		if r > 32767 { r = 32767 } else if r < -32768 { r = -32768 }
		out[i*2], out[i*2+1] = int16(l), int16(r)
		if math.Abs(l) > mp { mp = math.Abs(l) }
		if math.Abs(r) > mp { mp = math.Abs(r) }
	}
	m.MasterPeak = mp / 32768.0
	return out
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func main() {
	port := flag.Int("port", 8080, "Web port")
	flag.Parse()
	setRealtimePriority()
	
	link := abletonlink.NewLinkWithName(120.0, "WebMixer")
	defer link.Destroy()
	link.Enable(true); link.EnableAudio(true); link.EnableStartStopSync(true)
	
	dummySink := link.NewSink("WebMixer", 2048)
	defer dummySink.Destroy()

	mixer := &Mixer{MasterVolume: 1.0, Channels: make(map[string]*ChannelStrip), Link: link}
	hub := newHub()
	
	go func() {
		for {
			channels := link.Channels()
			mixer.mu.Lock()
			for _, ch := range channels {
				id := fmt.Sprintf("%d", ch.ID)
				if _, ok := mixer.Channels[id]; !ok {
					log.Printf("[Link] New channel: %s (Peer: %s)", ch.Name, ch.PeerName)
					strip := &ChannelStrip{ID: id, Name: ch.Name, PeerName: ch.PeerName, Volume: 0.8, buffer: make([]int16, 0, 8192)}
					strip.resamp = NewResampler(48000, SampleRate, Channels)
					s := strip
					strip.source = link.NewSource(ch.ID, func(samples []int16, info abletonlink.SourceBufferInfo) {
						if int(info.SampleRate) != s.resamp.inRate { s.resamp = NewResampler(int(info.SampleRate), SampleRate, Channels) }
						res := s.resamp.Resample(samples)
						if res != nil { s.Push(res) }
					})
					mixer.Channels[id] = strip
				}
			}
			mixer.mu.Unlock()
			time.Sleep(time.Second)
		}
	}()
	go func() {
		ticker := time.NewTicker(time.Duration(float64(FrameSize)/float64(SampleRate)*1000) * time.Millisecond)
		for range ticker.C {
			pcm := mixer.Process(FrameSize)
			mixer.streamMu.Lock()
			if mixer.streaming && mixer.streamConn != nil { mixer.streamEnc.Write(mixer.streamConn, pcm) }
			mixer.streamMu.Unlock()
			if hub.hasMonitor() {
				f32 := make([]float32, len(pcm))
				for i, s := range pcm { f32[i] = float32(s) / 32768.0 }
				buf := new(bytes.Buffer); binary.Write(buf, binary.LittleEndian, f32)
				hub.broadcastBinary(buf.Bytes())
			}
		}
	}()
	go hub.run(mixer)
	http.Handle("/", http.FileServer(http.FS(content)))
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) { serveWs(hub, mixer, w, r) })
	log.Printf("Web Mixer Listening on http://localhost:%d", *port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", *port), nil))
}

// -- Hub --

type Hub struct {
	clients map[*Client]bool
	register, unregister chan *Client
	monCount int32
}

type Client struct {
	hub *Hub
	conn *websocket.Conn
	send chan []byte
	mon bool
}

func newHub() *Hub { return &Hub{clients: make(map[*Client]bool), register: make(chan *Client), unregister: make(chan *Client)} }
func (h *Hub) hasMonitor() bool { return atomic.LoadInt32(&h.monCount) > 0 }
func (h *Hub) broadcastBinary(d []byte) {
	for c := range h.clients { if c.mon { select { case c.send <- d: default: } } }
}
func (h *Hub) run(m *Mixer) {
	ticker := time.NewTicker(100 * time.Millisecond)
	for {
		select {
		case c := <-h.register: h.clients[c] = true
		case c := <-h.unregister:
			if _, ok := h.clients[c]; ok {
				if c.mon { atomic.AddInt32(&h.monCount, -1) }
				delete(h.clients, c); close(c.send)
			}
		case <-ticker.C:
			st := abletonlink.NewSessionState(); m.Link.CaptureAppSessionState(st)
			bpm, playing := st.Tempo(), st.IsPlaying(); st.Destroy()
			m.mu.RLock(); m.streamMu.Lock()
			msg := MixerState{BPM: bpm, Playing: playing, Streaming: m.streaming, Peers: m.Link.NumPeers(), Master: ChannelState{Volume: m.MasterVolume, Muted: m.MasterMuted}, Channels: []ChannelState{}}
			m.streamMu.Unlock()
			mets := make(map[string]float64); mets["master"] = m.MasterPeak
			for _, ch := range m.Channels {
				msg.Channels = append(msg.Channels, ChannelState{ID: ch.ID, Name: ch.Name, PeerName: ch.PeerName, Volume: ch.Volume, Muted: ch.Muted, Soloed: ch.Soloed})
				mets[ch.ID] = ch.PeakHold; ch.PeakHold = 0
			}
			m.mu.RUnlock()
			sort.Slice(msg.Channels, func(i, j int) bool { return msg.Channels[i].Name < msg.Channels[j].Name })
			
			bS, _ := json.Marshal(WSMessage{Type: "state", Data: mustMarshal(msg)})
			bM, _ := json.Marshal(WSMessage{Type: "meters", Data: mustMarshal(mets)})
			for c := range h.clients {
				select { case c.send <- bS: default: }
				select { case c.send <- bM: default: }
			}
		}
	}
}

func mustMarshal(v interface{}) []byte { b, _ := json.Marshal(v); return b }
func serveWs(h *Hub, m *Mixer, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil { return }
	c := &Client{hub: h, conn: conn, send: make(chan []byte, 1024)}
	h.register <- c
	go func() {
		defer func() { c.hub.unregister <- c; c.conn.Close() }()
		for {
			_, message, err := c.conn.ReadMessage()
			if err != nil { break }
			var msg WSMessage
			if err := json.Unmarshal(message, &msg); err == nil {
				switch msg.Type {
				case "volume":
					var cmd VolumeCmd
					if json.Unmarshal(msg.Data, &cmd) == nil {
						m.mu.Lock()
						if cmd.ID == "master" { m.MasterVolume = cmd.Value } else if ch, ok := m.Channels[cmd.ID]; ok { ch.Volume = cmd.Value }
						m.mu.Unlock()
					}
				case "mute":
					var cmd BoolCmd
					if json.Unmarshal(msg.Data, &cmd) == nil {
						m.mu.Lock()
						if cmd.ID == "master" { m.MasterMuted = cmd.Value } else if ch, ok := m.Channels[cmd.ID]; ok { ch.Muted = cmd.Value }
						m.mu.Unlock()
					}
				case "solo":
					var cmd BoolCmd
					if json.Unmarshal(msg.Data, &cmd) == nil {
						m.mu.Lock()
						if ch, ok := m.Channels[cmd.ID]; ok { ch.Soloed = cmd.Value }
						m.mu.Unlock()
					}
				case "transport":
					var cmd TransportCmd
					if json.Unmarshal(msg.Data, &cmd) == nil {
						st := abletonlink.NewSessionState(); m.Link.CaptureAppSessionState(st); st.SetIsPlaying(cmd.Playing, m.Link.ClockMicros()); m.Link.CommitAppSessionState(st); st.Destroy()
					}
				case "monitor":
					var cmd MonitorCmd
					if json.Unmarshal(msg.Data, &cmd) == nil {
						if cmd.Enabled && !c.mon { atomic.AddInt32(&h.monCount, 1) } else if !cmd.Enabled && c.mon { atomic.AddInt32(&h.monCount, -1) }
						c.mon = cmd.Enabled
					}
				case "start_stream":
					var cfg StreamConfig
					if json.Unmarshal(msg.Data, &cfg) == nil { go m.StartStream(cfg) }
				case "stop_stream": m.StopStream()
				}
			}
		}
	}()
	for {
		select {
		case msg, ok := <-c.send:
			if !ok { c.conn.WriteMessage(websocket.CloseMessage, []byte{}); return }
			if json.Valid(msg) { c.conn.WriteMessage(websocket.TextMessage, msg) } else { c.conn.WriteMessage(websocket.BinaryMessage, msg) }
		}
	}
}
