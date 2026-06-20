// diagnostics.go — network diagnostics endpoints.
//
// Routes (all require auth):
//
//	POST /api/diagnostics/ping               ← ping a host, return reachable/latency/loss
//	GET  /api/diagnostics/ping/stream        ← SSE streaming ping with terminal-style output
//	GET  /api/diagnostics/traceroute/stream  ← SSE streaming traceroute
//	GET  /api/diagnostics/tcpdump/stream     ← SSE streaming tcpdump
//	GET  /api/diagnostics/tcpdump/download   ← download previously saved PCAP file
package api

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
)

// tcpdumpCapture tracks an active or completed PCAP save session.
type tcpdumpCapture struct {
	path    string       // absolute path to the .pcap file
	process *os.Process  // nil until tcpdump starts
	done    chan struct{} // closed when tcpdump exits and file is finalized
}

var (
	tcpdumpCapsMu sync.Mutex
	tcpdumpCaps   = map[string]*tcpdumpCapture{}
)

func RegisterDiagnostics(api fiber.Router) {
	g := api.Group("/diagnostics")
	g.Post("/ping", diagnosticsPing)
	g.Get("/ping/stream", diagnosticsPingStream)
	g.Get("/traceroute/stream", diagnosticsTracerouteStream)
	g.Get("/tcpdump/stream", diagnosticsTcpdumpStream)
	g.Post("/tcpdump/stop", diagnosticsTcpdumpStop)
	g.Get("/tcpdump/download", diagnosticsTcpdumpDownload)
}

type PingRequest struct {
	Host  string `json:"host"`
	Count int    `json:"count"`
}

type PingResult struct {
	Reachable  bool    `json:"reachable"`
	LatencyMs  float64 `json:"latencyMs"`
	PacketLoss int     `json:"packetLoss"` // percent
}

var (
	reLoss = regexp.MustCompile(`(\d+)% packet loss`)
	reRTT  = regexp.MustCompile(`rtt min/avg/max/mdev = [\d.]+/([\d.]+)/[\d.]+/[\d.]+ ms`)
)

func diagnosticsPing(c *fiber.Ctx) error {
	var req PingRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}
	host := strings.TrimSpace(req.Host)
	if host == "" {
		return fiber.NewError(fiber.StatusBadRequest, "host is required")
	}
	for _, ch := range host {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
			(ch >= '0' && ch <= '9') || ch == '.' || ch == '-' || ch == ':' || ch == '[' || ch == ']' {
			continue
		}
		return fiber.NewError(fiber.StatusBadRequest, fmt.Sprintf("invalid host: %q", host))
	}
	count := req.Count
	if count <= 0 || count > 10 {
		count = 3
	}

	cmd := exec.Command("ping", "-c", strconv.Itoa(count), "-W", "2", host)
	out, _ := cmd.Output() // ignore exit code — non-zero on packet loss

	result := parsePingOutput(string(out))
	if cmd.ProcessState != nil && cmd.ProcessState.ExitCode() == 1 && result.PacketLoss == 100 {
		result.Reachable = false
	}

	// Timeout guard.
	timeout := time.Duration(count*3) * time.Second
	done := make(chan struct{}, 1)
	go func() { done <- struct{}{} }()
	select {
	case <-done:
	case <-time.After(timeout):
		return c.JSON(&PingResult{Reachable: false, PacketLoss: 100})
	}

	return c.JSON(result)
}

func parsePingOutput(out string) *PingResult {
	res := &PingResult{PacketLoss: 100}

	if m := reLoss.FindStringSubmatch(out); len(m) > 1 {
		loss, _ := strconv.Atoi(m[1])
		res.PacketLoss = loss
		res.Reachable = loss < 100
	}
	if m := reRTT.FindStringSubmatch(out); len(m) > 1 {
		avg, _ := strconv.ParseFloat(m[1], 64)
		res.LatencyMs = avg
	}
	return res
}

// diagnosticsPingStream runs ping and streams output line-by-line via SSE.
// Query params:
//
//	host     — destination (required)
//	count    — packet count, 1–20 (default 5)
//	source   — source interface name (optional, passed as -I)
//	size     — payload bytes (optional, passed as -s)
//	df       — "true" to set Don't Fragment bit (-M do)
//	tos      — Type of Service byte 0–255 (optional, passed as -Q)
func diagnosticsPingStream(c *fiber.Ctx) error {
	host := strings.TrimSpace(c.Query("host"))
	if host == "" {
		return fiber.NewError(fiber.StatusBadRequest, "host is required")
	}
	for _, ch := range host {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
			(ch >= '0' && ch <= '9') || ch == '.' || ch == '-' || ch == ':' || ch == '[' || ch == ']' {
			continue
		}
		return fiber.NewError(fiber.StatusBadRequest, fmt.Sprintf("invalid host: %q", host))
	}

	count := 5
	if n, err := strconv.Atoi(c.Query("count")); err == nil && n >= 1 && n <= 20 {
		count = n
	}

	args := []string{"-c", strconv.Itoa(count), "-W", "2"}

	if src := c.Query("source"); src != "" {
		// Validate interface name: alphanumeric, dash, dot, underscore only.
		ok := true
		for _, ch := range src {
			if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
				(ch >= '0' && ch <= '9') || ch == '-' || ch == '.' || ch == '_') {
				ok = false
				break
			}
		}
		if ok {
			args = append(args, "-I", src)
		}
	}

	if sz, err := strconv.Atoi(c.Query("size")); err == nil && sz >= 0 && sz <= 65507 {
		args = append(args, "-s", strconv.Itoa(sz))
	}

	if c.Query("df") == "true" {
		args = append(args, "-M", "do")
	}

	if tos, err := strconv.Atoi(c.Query("tos")); err == nil && tos >= 0 && tos <= 255 {
		args = append(args, "-Q", strconv.Itoa(tos))
	}

	args = append(args, host)

	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Set("X-Accel-Buffering", "no")

	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		sendLine := func(line string) {
			fmt.Fprintf(w, "data: %s\n\n", line)
			w.Flush()
		}

		// stdbuf -oL forces line-buffered stdout so each ping reply
		// is streamed immediately rather than batched at the end.
		cmd := exec.Command("stdbuf", append([]string{"-oL", "ping"}, args...)...)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			sendLine("error: failed to start ping: " + err.Error())
			sendLine("[done]")
			return
		}
		stderr, _ := cmd.StderrPipe()

		timeout := time.Duration(count*4) * time.Second
		timer := time.AfterFunc(timeout, func() { cmd.Process.Kill() }) //nolint:errcheck
		defer timer.Stop()

		if err := cmd.Start(); err != nil {
			sendLine("error: " + err.Error())
			sendLine("[done]")
			return
		}

		// Stream stdout line by line.
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			sendLine(scanner.Text())
		}
		// Flush any stderr (e.g. "ping: unknown host").
		if stderr != nil {
			errScanner := bufio.NewScanner(stderr)
			for errScanner.Scan() {
				sendLine(errScanner.Text())
			}
		}

		cmd.Wait() //nolint:errcheck
		sendLine("[done]")
	})

	return nil
}

// diagnosticsTracerouteStream runs traceroute and streams output line-by-line via SSE.
// Query params:
//
//	host   — destination (required)
//	source — source interface IP (optional, passed as -s)
//	type   — "udp" (default) | "icmp" | "tcp"
func diagnosticsTracerouteStream(c *fiber.Ctx) error {
	host := strings.TrimSpace(c.Query("host"))
	if host == "" {
		return fiber.NewError(fiber.StatusBadRequest, "host is required")
	}
	for _, ch := range host {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
			(ch >= '0' && ch <= '9') || ch == '.' || ch == '-' || ch == ':' || ch == '[' || ch == ']' {
			continue
		}
		return fiber.NewError(fiber.StatusBadRequest, fmt.Sprintf("invalid host: %q", host))
	}

	args := []string{"-n"} // no DNS reverse lookup — faster output

	switch c.Query("type") {
	case "icmp":
		args = append(args, "-I")
	case "tcp":
		args = append(args, "-T")
	// udp is default — no flag needed
	}

	if src := c.Query("source"); src != "" {
		ok := true
		for _, ch := range src {
			if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
				(ch >= '0' && ch <= '9') || ch == '.' || ch == '-' || ch == '_') {
				ok = false
				break
			}
		}
		if ok {
			args = append(args, "-s", src)
		}
	}

	args = append(args, host)

	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Set("X-Accel-Buffering", "no")

	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		sendLine := func(line string) {
			fmt.Fprintf(w, "data: %s\n\n", line)
			w.Flush()
		}

		cmd := exec.Command("stdbuf", append([]string{"-oL", "traceroute"}, args...)...)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			sendLine("error: failed to start traceroute: " + err.Error())
			sendLine("[done]")
			return
		}
		stderr, _ := cmd.StderrPipe()

		timer := time.AfterFunc(120*time.Second, func() { cmd.Process.Kill() }) //nolint:errcheck
		defer timer.Stop()

		if err := cmd.Start(); err != nil {
			sendLine("error: " + err.Error())
			sendLine("[done]")
			return
		}

		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			sendLine(scanner.Text())
		}
		if stderr != nil {
			errScanner := bufio.NewScanner(stderr)
			for errScanner.Scan() {
				sendLine(errScanner.Text())
			}
		}

		cmd.Wait() //nolint:errcheck
		sendLine("[done]")
	})

	return nil
}

// diagnosticsTcpdumpStream runs tcpdump and streams output line-by-line via SSE.
// Query params:
//
//	iface  — network interface to capture on (required)
//	filter — optional BPF filter expression (e.g. "host 8.8.8.8 and port 53")
//	save   — "true" to save capture to a temp PCAP file
//
// When save=true the FIRST message is [captureid:<id>] so the frontend knows
// the ID immediately and can trigger a stop + download without waiting for [done].
func diagnosticsTcpdumpStream(c *fiber.Ctx) error {
	iface := strings.TrimSpace(c.Query("iface"))
	if iface == "" {
		return fiber.NewError(fiber.StatusBadRequest, "iface is required")
	}
	for _, ch := range iface {
		if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
			(ch >= '0' && ch <= '9') || ch == '-' || ch == '.' || ch == '_') {
			return fiber.NewError(fiber.StatusBadRequest, fmt.Sprintf("invalid interface: %q", iface))
		}
	}

	savePcap := c.Query("save") == "true"

	// BPF filter words (validated, no shell injection via separate args).
	var filterArgs []string
	if filter := strings.TrimSpace(c.Query("filter")); filter != "" {
		filterArgs = strings.Fields(filter)
	}

	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Set("X-Accel-Buffering", "no")

	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		sendLine := func(line string) {
			fmt.Fprintf(w, "data: %s\n\n", line)
			w.Flush()
		}

		if savePcap {
			// Generate capture ID and register capture entry BEFORE starting process
			// so the frontend receives the ID as the very first SSE event.
			idBytes := make([]byte, 12)
			rand.Read(idBytes) //nolint:errcheck
			captureID := hex.EncodeToString(idBytes)
			pcapPath := "/tmp/cascade-" + captureID + ".pcap"

			cap := &tcpdumpCapture{
				path: pcapPath,
				done: make(chan struct{}),
			}
			tcpdumpCapsMu.Lock()
			tcpdumpCaps[captureID] = cap
			tcpdumpCapsMu.Unlock()

			// Send ID first so frontend has it before any blocking I/O.
			sendLine("[captureid:" + captureID + "]")

			args := []string{"-n", "-i", iface, "-w", pcapPath}
			args = append(args, filterArgs...)
			cmd := exec.Command("tcpdump", args...)

			stderr, err := cmd.StderrPipe()
			if err != nil {
				sendLine("error: failed to start tcpdump: " + err.Error())
				sendLine("[done]")
				close(cap.done)
				return
			}

			timer := time.AfterFunc(300*time.Second, func() { cmd.Process.Kill() }) //nolint:errcheck
			defer timer.Stop()

			if err := cmd.Start(); err != nil {
				sendLine("error: " + err.Error())
				sendLine("[done]")
				close(cap.done)
				return
			}

			// Store process handle so stop endpoint can send SIGINT.
			tcpdumpCapsMu.Lock()
			cap.process = cmd.Process
			tcpdumpCapsMu.Unlock()

			sendLine("Saving capture to PCAP… (Stop to finalize)")

			errScanner := bufio.NewScanner(stderr)
			for errScanner.Scan() {
				sendLine(errScanner.Text())
			}

			cmd.Wait() //nolint:errcheck

			// Signal stop endpoint (if waiting) that file is finalized.
			close(cap.done)

			if _, statErr := os.Stat(pcapPath); statErr != nil {
				// File not created (e.g. permission error) — remove from registry.
				tcpdumpCapsMu.Lock()
				delete(tcpdumpCaps, captureID)
				tcpdumpCapsMu.Unlock()
				sendLine("error: pcap file not created")
			}

			sendLine("[done]")

		} else {
			// Live text mode: -l line-buffered stdout, -n no DNS.
			args := []string{"-l", "-n", "-i", iface}
			args = append(args, filterArgs...)
			cmd := exec.Command("tcpdump", args...)

			stdout, err := cmd.StdoutPipe()
			if err != nil {
				sendLine("error: failed to start tcpdump: " + err.Error())
				sendLine("[done]")
				return
			}
			stderr, _ := cmd.StderrPipe()

			timer := time.AfterFunc(300*time.Second, func() { cmd.Process.Kill() }) //nolint:errcheck
			defer timer.Stop()

			if err := cmd.Start(); err != nil {
				sendLine("error: " + err.Error())
				sendLine("[done]")
				return
			}

			tcpScanner := bufio.NewScanner(stdout)
			for tcpScanner.Scan() {
				sendLine(tcpScanner.Text())
			}
			if stderr != nil {
				errScanner := bufio.NewScanner(stderr)
				for errScanner.Scan() {
					sendLine(errScanner.Text())
				}
			}

			cmd.Wait() //nolint:errcheck
			sendLine("[done]")
		}
	})

	return nil
}

// diagnosticsTcpdumpStop sends SIGINT to a running PCAP capture and waits for
// the process to exit so the file is fully flushed before the client downloads it.
// Query params:
//
//	file — capture ID from [captureid:<id>] SSE event
func diagnosticsTcpdumpStop(c *fiber.Ctx) error {
	id := strings.TrimSpace(c.Query("file"))
	if !validCaptureID(id) {
		return fiber.NewError(fiber.StatusBadRequest, "invalid file id")
	}

	tcpdumpCapsMu.Lock()
	cap, ok := tcpdumpCaps[id]
	tcpdumpCapsMu.Unlock()

	if !ok {
		// Already done or never existed — not an error from client's perspective.
		return c.JSON(fiber.Map{"ok": true})
	}

	// Send SIGINT so tcpdump flushes pcap headers and closes the file cleanly.
	if cap.process != nil {
		cap.process.Signal(os.Interrupt) //nolint:errcheck
	}

	// Wait for the stream goroutine to close cap.done (max 10s).
	select {
	case <-cap.done:
	case <-time.After(10 * time.Second):
		if cap.process != nil {
			cap.process.Kill() //nolint:errcheck
		}
	}

	return c.JSON(fiber.Map{"ok": true})
}

// diagnosticsTcpdumpDownload serves a previously saved PCAP capture file.
// The file is deleted from disk and from the in-memory registry after download.
// Query params:
//
//	file — capture ID from [captureid:<id>] SSE event
func diagnosticsTcpdumpDownload(c *fiber.Ctx) error {
	id := strings.TrimSpace(c.Query("file"))
	if !validCaptureID(id) {
		return fiber.NewError(fiber.StatusBadRequest, "invalid file id")
	}

	tcpdumpCapsMu.Lock()
	cap, ok := tcpdumpCaps[id]
	if ok {
		delete(tcpdumpCaps, id)
	}
	tcpdumpCapsMu.Unlock()

	if !ok {
		return fiber.NewError(fiber.StatusNotFound, "capture not found or already downloaded")
	}

	c.Set("Content-Disposition", `attachment; filename="capture-`+id[:8]+`.pcap"`)
	c.Set("Content-Type", "application/vnd.tcpdump.pcap")
	err := c.SendFile(cap.path)
	os.Remove(cap.path) //nolint:errcheck
	return err
}

// validCaptureID returns true for non-empty lowercase hex strings (hex.EncodeToString output).
func validCaptureID(id string) bool {
	if id == "" {
		return false
	}
	for _, ch := range id {
		if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f')) {
			return false
		}
	}
	return true
}
