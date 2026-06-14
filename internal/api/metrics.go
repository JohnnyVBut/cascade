// metrics.go — real-time and historical system metrics endpoints.
//
// Routes (all require auth):
//
//	GET /api/metrics              ← current snapshot (CPU, RAM, net interfaces)
//	GET /api/metrics/history      ← aggregated history from SQLite
//	  ?key=cpu&from=<unix>&to=<unix>&period=5m|1h|6h|24h|7d|30d
package api

import (
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/JohnnyVBut/cascade/internal/metrics"
)

func RegisterMetrics(api fiber.Router) {
	g := api.Group("/metrics")
	g.Get("/", metricsSnapshot)
	g.Get("/history", metricsHistory)
	g.Get("/gateway-dist", metricsGatewayDist)
}

// GET /api/metrics
func metricsSnapshot(c *fiber.Ctx) error {
	snap := metrics.Current()
	if snap == nil {
		return c.JSON(fiber.Map{
			"cpu": 0, "mem": 0, "memUsedMb": 0, "memTotalMb": 0,
			"net": fiber.Map{}, "interfaces": []string{},
		})
	}

	netMap := fiber.Map{}
	for iface, ns := range snap.Net {
		netMap[iface] = fiber.Map{
			"rxMbps": ns.RxMbps,
			"txMbps": ns.TxMbps,
		}
	}

	gwMap := fiber.Map{}
	for id, status := range snap.Gateways {
		gwMap[id] = status
	}

	return c.JSON(fiber.Map{
		"cpu":        snap.CPU,
		"mem":        snap.MemUsedPct,
		"memUsedMb":  snap.MemUsedMB,
		"memTotalMb": snap.MemTotalMB,
		"net":        netMap,
		"interfaces": snap.Interfaces,
		"gateways":   gwMap,
	})
}

// GET /api/metrics/gateway-dist?key=gateway:<id>&period=1h
// Returns per-bucket status distribution for gateway bar charts.
// Each bucket: [ts_ms, healthy_count, degraded_count, down_count, admin_down_count]
func metricsGatewayDist(c *fiber.Ctx) error {
	key := c.Query("key")
	if key == "" {
		return fiber.NewError(fiber.StatusBadRequest, "key is required")
	}
	if !strings.HasPrefix(key, "gateway:") {
		return fiber.NewError(fiber.StatusBadRequest, "key must start with gateway:")
	}

	period := c.Query("period", "1h")
	now := time.Now().Unix()

	var from int64
	var stepSec int
	switch period {
	case "5m":
		from = now - 300
		stepSec = 5
	case "1h":
		from = now - 3600
		stepSec = 60
	case "6h":
		from = now - 6*3600
		stepSec = 300
	case "24h":
		from = now - 24*3600
		stepSec = 900
	case "7d":
		from = now - 7*24*3600
		stepSec = 3600
	case "30d":
		from = now - 30*24*3600
		stepSec = 6 * 3600
	default:
		return fiber.NewError(fiber.StatusBadRequest, "invalid period")
	}

	buckets, err := metrics.GatewayDistribution(key, from, now, stepSec)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	if buckets == nil {
		buckets = [][5]float64{}
	}

	return c.JSON(fiber.Map{"key": key, "period": period, "buckets": buckets})
}

// GET /api/metrics/history?key=cpu&from=<unix>&to=<unix>&period=5m
func metricsHistory(c *fiber.Ctx) error {
	key := c.Query("key")
	if key == "" {
		return fiber.NewError(fiber.StatusBadRequest, "key is required")
	}

	period := c.Query("period", "5m")
	now := time.Now().Unix()

	var from int64
	var stepSec int
	switch period {
	case "5m":
		from = now - 300
		stepSec = 5
	case "1h":
		from = now - 3600
		stepSec = 60
	case "6h":
		from = now - 6*3600
		stepSec = 300
	case "24h":
		from = now - 24*3600
		stepSec = 900
	case "7d":
		from = now - 7*24*3600
		stepSec = 3600
	case "30d":
		from = now - 30*24*3600
		stepSec = 6 * 3600
	default:
		return fiber.NewError(fiber.StatusBadRequest, "invalid period")
	}

	points, err := metrics.History(key, from, now, stepSec)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	if points == nil {
		points = [][2]float64{}
	}

	return c.JSON(fiber.Map{"key": key, "period": period, "points": points})
}
