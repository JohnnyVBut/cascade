// speedtest.go — on-demand iperf3 speed test between any two Cascade servers.
//
// Routes (all require auth):
//
//	GET    /api/speedtest/check         ← check if iperf3 is installed
//	POST   /api/speedtest/run           ← start async test, return {jobId}
//	GET    /api/speedtest/result/:jobId ← poll status/result
//	GET    /api/speedtest/results       ← full history from DB
//	DELETE /api/speedtest/results       ← clear history
//
// Internal routes (called by the orchestrating server on behalf of the UI):
//
//	POST   /api/speedtest/server        ← start iperf3 -s, return {port, sessionId}
//	DELETE /api/speedtest/server/:id    ← kill iperf3 server process
//	POST   /api/speedtest/client        ← run iperf3 -c, return results
package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/JohnnyVBut/cascade/internal/db"
	"github.com/JohnnyVBut/cascade/internal/remotes"
)

// RegisterSpeedtest registers all /api/speedtest/* routes under the given auth-protected router.
func RegisterSpeedtest(api fiber.Router) {
	g := api.Group("/speedtest")
	g.Get("/check", speedtestCheck)
	g.Post("/run", speedtestRun)
	g.Get("/result/:jobId", speedtestGetResult)
	g.Get("/results", speedtestListResults)
	g.Delete("/results", speedtestClearResults)
	// Internal: called by orchestration on the target server via proxy.
	g.Post("/server", speedtestStartServer)
	g.Delete("/server/:sessionId", speedtestStopServer)
	g.Post("/client", speedtestRunClient)
}

// ── iperf3 server session store (in-memory, keyed by sessionId) ──────────────

type speedtestSession struct {
	cmd  *exec.Cmd
	port int
}

var (
	stMu       sync.Mutex
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

// ── DB helpers ────────────────────────────────────────────────────────────────

type SpeedtestRecord struct {
	ID          string   `json:"id"`
	FromServer  string   `json:"fromServer"`
	ToServer    string   `json:"toServer"`
	Host        string   `json:"host"`
	Port        int      `json:"port"`
	Duration    int      `json:"duration"`
	Streams     int      `json:"streams"`
	Status      string   `json:"status"`
	Via         string   `json:"via"` // "tunnel" | "internet"
	SendMbps    *float64 `json:"sendMbps"`
	RecvMbps    *float64 `json:"recvMbps"`
	Retransmits *int     `json:"retransmits"`
	LatencyMs   *float64 `json:"latencyMs"`
	Error       *string  `json:"error"`
	StartedAt   string   `json:"startedAt"`
	FinishedAt  *string  `json:"finishedAt"`
}

func stDBInsert(r SpeedtestRecord) error {
	via := r.Via
	if via == "" {
		via = "internet"
	}
	_, err := db.DB().Exec(`
		INSERT INTO speedtest_results
		  (id, from_server, to_server, host, port, duration, streams, status, via, started_at)
		VALUES (?,?,?,?,?,?,?,'running',?,datetime('now'))`,
		r.ID, r.FromServer, r.ToServer, r.Host, r.Port, r.Duration, r.Streams, via,
	)
	return err
}

func stDBComplete(id string, res *SpeedtestResult, errMsg string) {
	if errMsg != "" {
		db.DB().Exec(`UPDATE speedtest_results SET status='error', error=?, finished_at=datetime('now') WHERE id=?`, errMsg, id) //nolint:errcheck
		return
	}
	db.DB().Exec(`UPDATE speedtest_results
		SET status='done', send_mbps=?, recv_mbps=?, retransmits=?, latency_ms=?, finished_at=datetime('now')
		WHERE id=?`,
		res.SendMbps, res.RecvMbps, res.Retransmits, res.LatencyMs, id) //nolint:errcheck
}

func stDBGet(id string) (*SpeedtestRecord, error) {
	row := db.DB().QueryRow(`SELECT id, from_server, to_server, host, port, duration, streams,
		status, via, send_mbps, recv_mbps, retransmits, latency_ms, error, started_at, finished_at
		FROM speedtest_results WHERE id=?`, id)
	return stDBScan(row)
}

func stDBList() ([]SpeedtestRecord, error) {
	rows, err := db.DB().Query(`SELECT id, from_server, to_server, host, port, duration, streams,
		status, via, send_mbps, recv_mbps, retransmits, latency_ms, error, started_at, finished_at
		FROM speedtest_results ORDER BY started_at DESC LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []SpeedtestRecord
	for rows.Next() {
		r, err := stDBScan(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *r)
	}
	if list == nil {
		list = []SpeedtestRecord{}
	}
	return list, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func stDBScan(row rowScanner) (*SpeedtestRecord, error) {
	var r SpeedtestRecord
	var finishedAt sql.NullString
	err := row.Scan(
		&r.ID, &r.FromServer, &r.ToServer, &r.Host, &r.Port, &r.Duration, &r.Streams,
		&r.Status, &r.Via, &r.SendMbps, &r.RecvMbps, &r.Retransmits, &r.LatencyMs, &r.Error,
		&r.StartedAt, &finishedAt,
	)
	if err != nil {
		return nil, err
	}
	if finishedAt.Valid {
		r.FinishedAt = &finishedAt.String
	}
	return &r, nil
}

// ── HTTP handlers ─────────────────────────────────────────────────────────────

// GET /api/speedtest/check
func speedtestCheck(c *fiber.Ctx) error {
	path, err := exec.LookPath("iperf3")
	if err != nil {
		return c.JSON(fiber.Map{"installed": false})
	}
	return c.JSON(fiber.Map{"installed": true, "path": path})
}

// SpeedtestRunRequest is the body for POST /api/speedtest/run (orchestration endpoint).
type SpeedtestRunRequest struct {
	FromServer   string `json:"fromServer"`   // display name of source server
	ToServer     string `json:"toServer"`     // display name of destination server
	FromRemoteId string `json:"fromRemoteId"` // "" = local, else remote id
	ToRemoteId   string `json:"toRemoteId"`   // "" = local, else remote id
	Host         string `json:"host"`         // IP/hostname for iperf3 client to connect to
	BindAddr     string `json:"bindAddr"`     // optional: bind address for iperf3 client (to-interface IP)
	Via          string `json:"via"`          // "tunnel" | "internet"
	Duration     int    `json:"duration"`
	Streams      int    `json:"streams"`
}

// POST /api/speedtest/run
// Starts an async speed test job. The test runs in a goroutine; result is
// persisted to DB. Returns { jobId } immediately.
func speedtestRun(c *fiber.Ctx) error {
	var req SpeedtestRunRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}
	if strings.TrimSpace(req.Host) == "" {
		return fiber.NewError(fiber.StatusBadRequest, "host is required")
	}
	if req.Duration <= 0 {
		req.Duration = 10
	}
	if req.Streams <= 0 {
		req.Streams = 4
	}

	jobId := uuid.New().String()
	rec := SpeedtestRecord{
		ID:         jobId,
		FromServer: req.FromServer,
		ToServer:   req.ToServer,
		Host:       req.Host,
		Via:        req.Via,
		Duration:   req.Duration,
		Streams:    req.Streams,
	}
	if err := stDBInsert(rec); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "db insert: "+err.Error())
	}

	go stRunJob(jobId, req)

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"jobId": jobId})
}

// stRunJob orchestrates the full iperf3 test:
// 1. Check iperf3 on both sides
// 2. Start iperf3 server on fromRemote
// 3. Run iperf3 client on toRemote
// 4. Stop server
// 5. Persist result
func stRunJob(jobId string, req SpeedtestRunRequest) {
	fromId := req.FromRemoteId
	toId := req.ToRemoteId

	fail := func(msg string) { stDBComplete(jobId, nil, msg) }

	// 1. Check iperf3.
	chkFrom, err := stAPICheck(fromId)
	if err != nil || !chkFrom {
		fail("iperf3 not found on source server")
		return
	}
	chkTo, err := stAPICheck(toId)
	if err != nil || !chkTo {
		fail("iperf3 not found on destination server")
		return
	}

	// 2. Start server.
	port, sessionId, err := stAPIStartServer(fromId)
	if err != nil {
		fail("failed to start iperf3 server: " + err.Error())
		return
	}
	// Update port in DB so history shows it.
	db.DB().Exec(`UPDATE speedtest_results SET port=? WHERE id=?`, port, jobId) //nolint:errcheck

	defer func() {
		stAPIStopServer(fromId, sessionId) //nolint:errcheck
	}()

	// Small delay to let iperf3 server start listening.
	time.Sleep(500 * time.Millisecond)

	// 3. Run client.
	result, err := stAPIRunClient(toId, req.Host, port, req.Duration, req.Streams, req.BindAddr)
	if err != nil {
		fail(err.Error())
		return
	}

	stDBComplete(jobId, result, "")
}

// GET /api/speedtest/result/:jobId
func speedtestGetResult(c *fiber.Ctx) error {
	rec, err := stDBGet(c.Params("jobId"))
	if err == sql.ErrNoRows {
		return fiber.NewError(fiber.StatusNotFound, "job not found")
	}
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(rec)
}

// GET /api/speedtest/results
func speedtestListResults(c *fiber.Ctx) error {
	list, err := stDBList()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(fiber.Map{"results": list})
}

// DELETE /api/speedtest/results
func speedtestClearResults(c *fiber.Ctx) error {
	if _, err := db.DB().Exec(`DELETE FROM speedtest_results WHERE status != 'running'`); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ── Internal endpoints (iperf3 server/client control) ────────────────────────

// POST /api/speedtest/server
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
	stStore(sessionId, &speedtestSession{cmd: cmd, port: port})

	go func() {
		timer := time.NewTimer(90 * time.Second)
		defer timer.Stop()
		done := make(chan struct{})
		go func() { cmd.Wait(); close(done) }() //nolint:errcheck
		select {
		case <-timer.C:
			cmd.Process.Kill() //nolint:errcheck
		case <-done:
		}
		stPop(sessionId)
	}()

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"sessionId": sessionId, "port": port})
}

// DELETE /api/speedtest/server/:sessionId
func speedtestStopServer(c *fiber.Ctx) error {
	sess, ok := stPop(c.Params("sessionId"))
	if !ok {
		return c.SendStatus(fiber.StatusNoContent)
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
	Duration int    `json:"duration"`
	Streams  int    `json:"streams"`
	BindAddr string `json:"bindAddr"` // optional: --bind address for iperf3 client
}

// POST /api/speedtest/client
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
	result, err := runIperf3Client(req.Host, req.Port, duration, streams, req.BindAddr)
	if err != nil {
		return fiber.NewError(fiber.StatusBadGateway, err.Error())
	}
	return c.JSON(result)
}

// ── Internal API callers (used by stRunJob goroutine) ────────────────────────
// These call the local API directly (no HTTP) for the local server,
// or via a minimal HTTP call for remote servers.

func stAPICheck(remoteId string) (bool, error) {
	if remoteId == "" {
		_, err := exec.LookPath("iperf3")
		return err == nil, nil
	}
	res, err := stProxyCall(remoteId, "GET", "/speedtest/check", nil)
	if err != nil {
		return false, err
	}
	installed, _ := res["installed"].(bool)
	return installed, nil
}

func stAPIStartServer(remoteId string) (int, string, error) {
	if remoteId == "" {
		port, err := randomFreePort()
		if err != nil {
			return 0, "", err
		}
		cmd := exec.Command("iperf3", "-s", "--one-off", "-p", fmt.Sprintf("%d", port))
		if err := cmd.Start(); err != nil {
			return 0, "", err
		}
		sessionId := uuid.New().String()
		stStore(sessionId, &speedtestSession{cmd: cmd, port: port})
		go func() {
			timer := time.NewTimer(90 * time.Second)
			defer timer.Stop()
			done := make(chan struct{})
			go func() { cmd.Wait(); close(done) }() //nolint:errcheck
			select {
			case <-timer.C:
				cmd.Process.Kill() //nolint:errcheck
			case <-done:
			}
			stPop(sessionId)
		}()
		return port, sessionId, nil
	}
	res, err := stProxyCall(remoteId, "POST", "/speedtest/server", nil)
	if err != nil {
		return 0, "", err
	}
	port := int(res["port"].(float64))
	sessionId, _ := res["sessionId"].(string)
	return port, sessionId, nil
}

func stAPIStopServer(remoteId, sessionId string) error {
	if remoteId == "" {
		sess, ok := stPop(sessionId)
		if ok && sess.cmd.Process != nil {
			sess.cmd.Process.Kill() //nolint:errcheck
		}
		return nil
	}
	_, err := stProxyCall(remoteId, "DELETE", "/speedtest/server/"+sessionId, nil)
	return err
}

func stAPIRunClient(remoteId, host string, port, duration, streams int, bindAddr string) (*SpeedtestResult, error) {
	if remoteId == "" {
		return runIperf3Client(host, port, duration, streams, bindAddr)
	}
	body := map[string]any{"host": host, "port": port, "duration": duration, "streams": streams, "bindAddr": bindAddr}
	res, err := stProxyCall(remoteId, "POST", "/speedtest/client", body)
	if err != nil {
		return nil, err
	}
	// Re-parse the map into SpeedtestResult.
	b, _ := json.Marshal(res)
	var result SpeedtestResult
	if err := json.Unmarshal(b, &result); err != nil {
		return nil, fmt.Errorf("parse client result: %w", err)
	}
	return &result, nil
}

func newStHTTPRequest(method, url string, body *strings.Reader) (*http.Request, error) {
	return http.NewRequest(method, url, body)
}

// stProxyCall makes an authenticated HTTP call to a remote Cascade instance.
func stProxyCall(remoteId, method, path string, body map[string]any) (map[string]any, error) {
	r, err := remotes.Get(remoteId)
	if err != nil {
		return nil, fmt.Errorf("remote %s not found: %w", remoteId, err)
	}
	targetURL := strings.TrimRight(r.URL, "/") + "/api" + path

	var bodyReader *strings.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		bodyReader = strings.NewReader(string(b))
	} else {
		bodyReader = strings.NewReader("")
	}

	req, err := newStHTTPRequest(method, targetURL, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+r.Token)
	req.Header.Set("Content-Type", "application/json")

	// Use speedtestProxyClient for client calls (long timeout); proxyClient for the rest.
	// Use insecure variants for remotes with self-signed certificates.
	var client *http.Client
	switch {
	case method == "POST" && strings.HasSuffix(path, "/client") && r.SkipTLSVerify:
		client = speedtestProxyClientInsecure
	case method == "POST" && strings.HasSuffix(path, "/client"):
		client = speedtestProxyClient
	case r.SkipTLSVerify:
		client = proxyClientInsecure
	default:
		client = proxyClient
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 204 {
		return map[string]any{}, nil
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if resp.StatusCode >= 400 {
		msg, _ := result["message"].(string)
		if msg == "" {
			msg = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		return nil, fmt.Errorf("%s", msg)
	}
	return result, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func validateSpeedtestClientReq(req SpeedtestClientRequest) error {
	host := strings.TrimSpace(req.Host)
	if host == "" {
		return fmt.Errorf("host is required")
	}
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
	if req.BindAddr != "" {
		for _, ch := range req.BindAddr {
			if (ch >= '0' && ch <= '9') || ch == '.' {
				continue
			}
			return fmt.Errorf("invalid bindAddr: %q", req.BindAddr)
		}
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

func runIperf3Client(host string, port, duration, streams int, bindAddr string) (*SpeedtestResult, error) {
	timeout := time.Duration(duration+15) * time.Second
	args := []string{
		"-c", host,
		"-p", fmt.Sprintf("%d", port),
		"-t", fmt.Sprintf("%d", duration),
		"-P", fmt.Sprintf("%d", streams),
		"-J",
	}
	if bindAddr != "" {
		args = append(args, "--bind", bindAddr)
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
		if r.err != nil && r.out == nil {
			return nil, fmt.Errorf("iperf3 failed: %w", r.err)
		}
		return parseIperf3JSON(r.out)
	case <-time.After(timeout):
		if cmd.Process != nil {
			cmd.Process.Kill() //nolint:errcheck
		}
		return nil, fmt.Errorf("iperf3 timed out after %ds", duration+15)
	}
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
				MeanRTT uint `json:"mean_rtt"`
			} `json:"sender"`
		} `json:"streams"`
	} `json:"end"`
	Error string `json:"error"`
}

// SpeedtestResult is the parsed iperf3 output returned to callers.
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
	if len(out.End.Streams) > 0 {
		var totalUs uint
		for _, s := range out.End.Streams {
			totalUs += s.Sender.MeanRTT
		}
		res.LatencyMs = float64(totalUs/uint(len(out.End.Streams))) / 1000.0
	}
	return res, nil
}
