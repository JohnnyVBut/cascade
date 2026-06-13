// diagnostics.go — network diagnostics endpoints.
//
// Routes (all require auth):
//
//	POST /api/diagnostics/ping  ← ping a host, return reachable/latency/loss
package api

import (
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
