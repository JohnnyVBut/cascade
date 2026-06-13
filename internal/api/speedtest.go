// speedtest.go — on-demand iperf3 speed test between any two Cascade servers.
//
// Routes (all require auth):
//
//	GET    /api/speedtest/check              ← check if iperf3 is installed
//	POST   /api/speedtest/server             ← start iperf3 -s, return {port, sessionId}
//	DELETE /api/speedtest/server/:sessionId  ← kill iperf3 server process
//	POST   /api/speedtest/client             ← run iperf3 -c, return results
package api

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// RegisterSpeedtest registers all /api/speedtest/* routes under the given auth-protected router.
func RegisterSpeedtest(api fiber.Router) {
	g := api.Group("/speedtest")
	g.Get("/check", speedtestCheck)
	g.Post("/server", speedtestStartServer)
	g.Delete("/server/:sessionId", speedtestStopServer)
	g.Post("/client", speedtestRunClient)
}

// ── session store ────────────────────────────────────────────────────────────

type speedtestSession struct {
	cmd  *exec.Cmd
	port int
}

var (
	stMu      sync.Mutex
	stSessions = make(map[string]*speedtestSession)
)

func stStore(id string, s *speedtestSession) {
	stMu.Lock()
	stSessions[id] = s
	stMu.Unlock()
}

func stPop(id string) (*speedtestSession, bool) {
	stMu.Lock()
	s, ok := stSessions[id]
	if ok {
		delete(stSessions, id)
	}
	stMu.Unlock()
	return s, ok
}

// ── handlers ─────────────────────────────────────────────────────────────────

// GET /api/speedtest/check
func speedtestCheck(c *fiber.Ctx) error {
	path, err := exec.LookPath("iperf3")
	if err != nil {
		return c.JSON(fiber.Map{"installed": false})
	}
	return c.JSON(fiber.Map{"installed": true, "path": path})
}

// POST /api/speedtest/server
// Starts iperf3 in server mode on a random high port. Auto-kills after 90s.
// Returns { sessionId, port }.
func speedtestStartServer(c *fiber.Ctx) error {
	port, err := randomFreePort()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not find free port: "+err.Error())
	}

	cmd := exec.Command("iperf3", "-s", "--one-off", "-p", fmt.Sprintf("%d", port))
	if err := cmd.Start(); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "iperf3 not found or failed to start: "+err.Error())
	}

	sessionId := uuid.New().String()
	sess := &speedtestSession{cmd: cmd, port: port}
	stStore(sessionId, sess)

	// Auto-kill after 90 s to avoid orphaned processes.
	go func() {
		timer := time.NewTimer(90 * time.Second)
		defer timer.Stop()
		done := make(chan struct{})
		go func() {
			cmd.Wait() //nolint:errcheck
			close(done)
		}()
		select {
		case <-timer.C:
			cmd.Process.Kill() //nolint:errcheck
		case <-done:
		}
		stPop(sessionId)
	}()

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"sessionId": sessionId,
		"port":      port,
	})
}

// DELETE /api/speedtest/server/:sessionId
func speedtestStopServer(c *fiber.Ctx) error {
	sess, ok := stPop(c.Params("sessionId"))
	if !ok {
		return c.SendStatus(fiber.StatusNoContent) // already gone — idempotent
	}
	if sess.cmd.Process != nil {
		sess.cmd.Process.Kill() //nolint:errcheck
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// SpeedtestClientRequest is the body for POST /api/speedtest/client.
type SpeedtestClientRequest struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Duration int    `json:"duration"` // seconds, 1-30
	Streams  int    `json:"streams"`  // parallel TCP streams, 1-16
}

// POST /api/speedtest/client
// Runs iperf3 client toward host:port, returns parsed results.
func speedtestRunClient(c *fiber.Ctx) error {
	var req SpeedtestClientRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}
	if err := validateSpeedtestClientReq(req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}

	duration := req.Duration
	if duration <= 0 {
		duration = 10
	}
	streams := req.Streams
	if streams <= 0 {
		streams = 4
	}

	// Timeout: test duration + 15 s overhead.
	timeout := time.Duration(duration+15) * time.Second

	args := []string{
		"-c", req.Host,
		"-p", fmt.Sprintf("%d", req.Port),
		"-t", fmt.Sprintf("%d", duration),
		"-P", fmt.Sprintf("%d", streams),
		"-J", // JSON output
	}
	cmd := exec.Command("iperf3", args...)

	type result struct {
		out []byte
		err error
	}
	ch := make(chan result, 1)
	go func() {
		out, err := cmd.Output()
		ch <- result{out, err}
	}()

	select {
	case r := <-ch:
		if r.err != nil {
			// iperf3 returns exit code 1 on connection refused etc. — still try to parse.
			if r.out == nil {
				return fiber.NewError(fiber.StatusBadGateway, "iperf3 failed: "+r.err.Error())
			}
		}
		parsed, parseErr := parseIperf3JSON(r.out)
		if parseErr != nil {
			return fiber.NewError(fiber.StatusBadGateway, "failed to parse iperf3 output: "+parseErr.Error())
		}
		return c.JSON(parsed)
	case <-time.After(timeout):
		cmd.Process.Kill() //nolint:errcheck
		return fiber.NewError(fiber.StatusGatewayTimeout, "iperf3 timed out")
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func validateSpeedtestClientReq(req SpeedtestClientRequest) error {
	host := strings.TrimSpace(req.Host)
	if host == "" {
		return fmt.Errorf("host is required")
	}
	// Accept IP or hostname — reject shell-special characters.
	for _, ch := range host {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
			(ch >= '0' && ch <= '9') || ch == '.' || ch == '-' || ch == ':' || ch == '[' || ch == ']' {
			continue
		}
		return fmt.Errorf("invalid host: %q", host)
	}
	if req.Port < 1024 || req.Port > 65535 {
		return fmt.Errorf("port must be between 1024 and 65535")
	}
	if req.Duration < 0 || req.Duration > 30 {
		return fmt.Errorf("duration must be between 1 and 30 seconds")
	}
	if req.Streams < 0 || req.Streams > 16 {
		return fmt.Errorf("streams must be between 1 and 16")
	}
	return nil
}

func randomFreePort() (int, error) {
	for range 10 {
		port := 20000 + rand.Intn(10000)
		l, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err == nil {
			l.Close()
			return port, nil
		}
	}
	return 0, fmt.Errorf("could not find a free port after 10 attempts")
}

// iperf3JSON mirrors the subset of iperf3 -J output we care about.
type iperf3JSON struct {
	End struct {
		SumSent struct {
			BitsPerSecond float64 `json:"bits_per_second"`
			Retransmits   int     `json:"retransmits"`
		} `json:"sum_sent"`
		SumReceived struct {
			BitsPerSecond float64 `json:"bits_per_second"`
		} `json:"sum_received"`
		Streams []struct {
			Sender struct {
				MeanRTT uint `json:"mean_rtt"` // microseconds
			} `json:"sender"`
		} `json:"streams"`
	} `json:"end"`
	Error string `json:"error"`
}

type SpeedtestResult struct {
	SendMbps    float64 `json:"sendMbps"`
	RecvMbps    float64 `json:"recvMbps"`
	Retransmits int     `json:"retransmits"`
	LatencyMs   float64 `json:"latencyMs"`
}

func parseIperf3JSON(data []byte) (*SpeedtestResult, error) {
	var out iperf3JSON
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	if out.Error != "" {
		return nil, fmt.Errorf("%s", out.Error)
	}
	res := &SpeedtestResult{
		SendMbps:    out.End.SumSent.BitsPerSecond / 1e6,
		RecvMbps:    out.End.SumReceived.BitsPerSecond / 1e6,
		Retransmits: out.End.SumSent.Retransmits,
	}
	// Average RTT across streams (microseconds → ms).
	if len(out.End.Streams) > 0 {
		var totalUs uint
		for _, s := range out.End.Streams {
			totalUs += s.Sender.MeanRTT
		}
		res.LatencyMs = float64(totalUs/uint(len(out.End.Streams))) / 1000.0
	}
	return res, nil
}
