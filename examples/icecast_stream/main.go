package main

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	abletonlink "github.com/DatanoiseTV/abletonlink-go"
	"github.com/braheezy/shine-mp3/pkg/mp3"
)

const AppVersion = "1.2.2-initial-metadata"

type IcecastConfig struct {
	Host        string
	Port        int
	Mount       string
	Password    string
	User        string
	Bitrate     int
	Name        string
	Description string
}

// Resampler handles linear resampling with state tracking
type Resampler struct {
	ratio    float64
	channels int
	position float64
	pending  []int16
}

func NewResampler(inRate, outRate, channels int) *Resampler {
	return &Resampler{
		ratio:    float64(inRate) / float64(outRate),
		channels: channels,
		pending:  make([]int16, 0, 4096),
	}
}

func (r *Resampler) Resample(input []int16) []int16 {
	if r.ratio == 1.0 {
		return input
	}

	r.pending = append(r.pending, input...)
	inFrames := len(r.pending) / r.channels
	
	if inFrames < 2 {
		return nil
	}

	numOutFrames := int((float64(inFrames) - 1 - r.position) / r.ratio)
	if numOutFrames <= 0 {
		return nil
	}

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

func isSupportedMP3Rate(rate int) bool {
	supported := []int{44100, 48000, 32000, 22050, 24000, 16000, 11025, 12000, 8000}
	for _, r := range supported {
		if rate == r {
			return true
		}
	}
	return false
}

type trackingWriter struct {
	io.Writer
	totalEncoded *int64
	bytesSent    *int64
}

func (tw trackingWriter) Write(p []byte) (n int, err error) {
	n, err = tw.Writer.Write(p)
	if tw.bytesSent != nil {
		atomic.AddInt64(tw.bytesSent, int64(n))
	}
	if tw.totalEncoded != nil {
		atomic.AddInt64(tw.totalEncoded, int64(n))
	}
	return n, err
}

func updateMetadata(config IcecastConfig, status string) {
	adminURL := fmt.Sprintf("http://%s:%d/admin/metadata", config.Host, config.Port)
	u, _ := url.Parse(adminURL)
	q := u.Query()
	q.Set("mount", "/"+config.Mount)
	q.Set("mode", "updinfo")
	q.Set("song", status)
	u.RawQuery = q.Encode()

	req, _ := http.NewRequest("GET", u.String(), nil)
	auth := base64.StdEncoding.EncodeToString([]byte(config.User + ":" + config.Password))
	req.Header.Set("Authorization", "Basic "+auth)

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err == nil {
		resp.Body.Close()
	}
}

func main() {
	fmt.Printf("=== Ableton Link to Icecast2 MP3 Streamer v%s ===\n", AppVersion)
	fmt.Println("This tool streams Link audio channels to Icecast2 as MP3.")
	fmt.Println()

	if err := setRealtimePriority(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to set real-time priority: %v\n", err)
	}

	config := wizard()

	link := abletonlink.NewLink(120.0)
	defer link.Destroy()
	link.Enable(true)
	link.EnableAudio(true)
	link.EnableStartStopSync(true)

	dummySink := link.NewSink("LinkGo-Icecast", 2048)
	defer dummySink.Destroy()

	var currentBPM int64 = 120000 // Fixed point BPM * 1000
	
	// Pre-start heartbeat to pump events
	go func() {
		state := abletonlink.NewSessionState()
		defer state.Destroy()
		for {
			link.CaptureAppSessionState(state)
			bpm := state.Tempo()
			atomic.StoreInt64(&currentBPM, int64(bpm*1000))
			time.Sleep(100 * time.Millisecond)
		}
	}()

	fmt.Println("\n[1] Discovering Link audio channels...")
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)

	var selectedChannel *abletonlink.Channel
	reader := bufio.NewReader(os.Stdin)

loop:
	for selectedChannel == nil {
		select {
		case <-sigChan:
			fmt.Println("\nAborted.")
			return
		default:
			channels := link.Channels()
			if len(channels) > 0 {
				fmt.Printf("\nFound %d channel(s):\n", len(channels))
				for i, ch := range channels {
					fmt.Printf("%d: %s (Peer: %s, ID: %d)\n", i, ch.Name, ch.PeerName, ch.ID)
				}
				fmt.Printf("\nEnter channel index to stream (or 'r' to refresh): ")
				input, _ := reader.ReadString('\n')
				input = strings.TrimSpace(input)
				if input == "r" || input == "" {
					continue
				}
				idx, err := strconv.Atoi(input)
				if err == nil && idx >= 0 && idx < len(channels) {
					selectedChannel = &channels[idx]
					break loop
				} else {
					fmt.Println("Invalid selection.")
				}
			} else {
				fmt.Print(".")
				time.Sleep(1 * time.Second)
			}
		}
	}

	fmt.Printf("\nSelected channel: %s from %s\n", selectedChannel.Name, selectedChannel.PeerName)
	fmt.Print("Start Link transport? [y/N]: ")
	if start, _ := reader.ReadString('\n'); strings.ToLower(strings.TrimSpace(start)) == "y" {
		state := abletonlink.NewSessionState()
		link.CaptureAppSessionState(state)
		state.SetIsPlaying(true, link.ClockMicros())
		link.CommitAppSessionState(state)
		state.Destroy()
		fmt.Println("Transport start requested.")
	}

	fmt.Println("Waiting for audio data to determine format...")

	audioChan := make(chan []int16, 2048)
	var formatOnce sync.Once
	formatReady := make(chan bool)
	var sampleRate uint32
	var numChannels uint64
	var buffersReceived int64
	var peak int32
	var totalEncoded int64

	source := link.NewSource(selectedChannel.ID, func(samples []int16, info abletonlink.SourceBufferInfo) {
		atomic.AddInt64(&buffersReceived, 1)
		
		var localPeak int16
		for _, s := range samples {
			val := s
			if val < 0 { val = -val }
			if val > localPeak { localPeak = val }
		}
		for {
			oldPeak := atomic.LoadInt32(&peak)
			if int32(localPeak) <= oldPeak { break }
			if atomic.CompareAndSwapInt32(&peak, oldPeak, int32(localPeak)) { break }
		}

		formatOnce.Do(func() {
			sampleRate = info.SampleRate
			numChannels = info.NumChannels
			close(formatReady)
		})

		s := make([]int16, len(samples))
		copy(s, samples)
		select {
		case audioChan <- s:
		default:
		}
	})
	defer source.Destroy()

	go func() {
		for {
			time.Sleep(1 * time.Second)
			count := atomic.LoadInt64(&buffersReceived)
			enc := atomic.LoadInt64(&totalEncoded)
			p := atomic.SwapInt32(&peak, 0)
			bpm := float64(atomic.LoadInt64(&currentBPM)) / 1000.0
			if count > 0 {
				level := float64(p) / 32768.0
				meter := ""
				for i := 0; i < 15; i++ {
					if float64(i)/15.0 < level { meter += "█" } else { meter += "░" }
				}
				fmt.Printf("\r[Status] In: %d | Level: [%s] | %.1f BPM | Out: %d KB   ", count, meter, bpm, enc/1024)
			}
		}
	}()

	select {
	case <-formatReady:
		fmt.Printf("\nDetected format: %d Hz, %d channels\n", sampleRate, numChannels)
	case <-time.After(30 * time.Second):
		fmt.Println("\nTimeout waiting for audio data.")
		return
	case <-sigChan:
		return
	}

	targetRate := int(sampleRate)
	if !isSupportedMP3Rate(targetRate) {
		targetRate = 44100
		fmt.Printf("Resampling: %d Hz -> %d Hz\n", sampleRate, targetRate)
	}
	
	resampler := NewResampler(int(sampleRate), targetRate, int(numChannels))
	mp3Encoder := mp3.NewEncoder(targetRate, int(numChannels))

	fmt.Printf("Connecting to Icecast at %s:%d/%s...\n", config.Host, config.Port, config.Mount)
	conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", config.Host, config.Port))
	if err != nil {
		fmt.Printf("Failed to connect to Icecast: %v\n", err)
		return
	}
	defer conn.Close()

	auth := base64.StdEncoding.EncodeToString([]byte(config.User + ":" + config.Password))
	header := fmt.Sprintf("SOURCE /%s HTTP/1.0\r\n"+
		"Authorization: Basic %s\r\n"+
		"Content-Type: audio/mpeg\r\n"+
		"Ice-Public: 0\r\n"+
		"Ice-Name: %s\r\n"+
		"Ice-Description: %s\r\n"+
		"Ice-Audio-Info: channels=%d;samplerate=%d;bitrate=%d\r\n"+
		"\r\n", config.Mount, auth, config.Name, config.Description, numChannels, targetRate, config.Bitrate)

	_, err = conn.Write([]byte(header))
	if err != nil {
		fmt.Printf("Failed to send handshake: %v\n", err)
		return
	}

	respReader := bufio.NewReader(conn)
	respLine, err := respReader.ReadString('\n')
	if err != nil || !strings.Contains(respLine, "200") {
		fmt.Printf("Icecast rejected connection: %s\n", respLine)
		return
	}
	fmt.Println("Icecast connected successfully! Streaming...")

	// START METADATA UPDATER NOW
	go func() {
		var lastReportedBPM float64
		// Force initial update
		bpm := float64(atomic.LoadInt64(&currentBPM)) / 1000.0
		status := fmt.Sprintf("%s - %.2f BPM", config.Name, bpm)
		updateMetadata(config, status)
		lastReportedBPM = bpm

		for {
			bpm := float64(atomic.LoadInt64(&currentBPM)) / 1000.0
			if math.Abs(bpm-lastReportedBPM) > 0.1 {
				lastReportedBPM = bpm
				status := fmt.Sprintf("%s - %.2f BPM", config.Name, bpm)
				updateMetadata(config, status)
			}
			time.Sleep(1 * time.Second)
		}
	}()

	var bytesSent int64
	tw := trackingWriter{
		Writer:       conn,
		totalEncoded: &totalEncoded,
		bytesSent:    &bytesSent,
	}
	frameSize := 1152
	sampleBuffer := make([]int16, 0, frameSize*int(numChannels)*4)

	for {
		select {
		case rawSamples := <-audioChan:
			samples := resampler.Resample(rawSamples)
			if samples != nil {
				sampleBuffer = append(sampleBuffer, samples...)
			}

			for len(sampleBuffer) >= frameSize*int(numChannels) {
				frame := sampleBuffer[:frameSize*int(numChannels)]
				sampleBuffer = sampleBuffer[frameSize*int(numChannels):]
				err = mp3Encoder.Write(tw, frame)
				if err != nil {
					fmt.Printf("\nStream error: %v\n", err)
					return
				}
			}
		case <-sigChan:
			fmt.Println("\nShutting down...")
			return
		}
	}
}

func wizard() IcecastConfig {
	reader := bufio.NewReader(os.Stdin)
	config := IcecastConfig{
		Host:        "localhost",
		Port:        8000,
		Mount:       "link.mp3",
		Password:    "hackme",
		User:        "source",
		Bitrate:     128,
		Name:        "icecast",
		Description: "Live from Ableton Link Go",
	}

	fmt.Printf("Icecast Host [%s]: ", config.Host)
	if val := readLine(reader); val != "" { config.Host = val }
	fmt.Printf("Icecast Port [%d]: ", config.Port)
	if val := readLine(reader); val != "" {
		if p, err := strconv.Atoi(val); err == nil { config.Port = p }
	}
	fmt.Printf("Mount Point [%s]: ", config.Mount)
	if val := readLine(reader); val != "" { config.Mount = strings.TrimPrefix(val, "/") }
	fmt.Printf("Icecast Password [%s]: ", config.Password)
	if val := readLine(reader); val != "" { config.Password = val }
	fmt.Printf("Station Name [%s]: ", config.Name)
	if val := readLine(reader); val != "" { config.Name = val }
	fmt.Printf("Station Description [%s]: ", config.Description)
	if val := readLine(reader); val != "" { config.Description = val }

	return config
}

func readLine(r *bufio.Reader) string {
	line, _ := r.ReadString('\n')
	return strings.TrimSpace(line)
}
