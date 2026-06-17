// diagnostics.go — network diagnostics endpoints.
//
// Routes (all require auth):
//
//	POST /api/diagnostics/ping          ← ping a host, return reachable/latency/loss
//	GET  /api/diagnostics/ping/stream   ← SSE streaming ping with terminal-style output
package api

import (
	"bufio"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

func RegisterDiagnostics(api fiber.Router) {
	g := api.Group("/diagnostics")
	g.Post("/ping", diagnosticsPing)
	g.Get("/ping/stream", diagnosticsPingStream)
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
