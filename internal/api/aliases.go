// aliases.go — HTTP handlers for AliasManager (firewall aliases).
//
// Routes:
//
//	GET    /api/aliases
//	POST   /api/aliases
//	GET    /api/aliases/:id
//	PATCH  /api/aliases/:id
//	DELETE /api/aliases/:id
//	POST   /api/aliases/:id/upload           ← upload prefix file → ipset
//	POST   /api/aliases/:id/generate         ← start async generation job, returns { jobId }
//	GET    /api/aliases/:id/generate/:jobId  ← poll job status { status, entryCount?, error? }
package api

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/gofiber/fiber/v2"

	"github.com/JohnnyVBut/cascade/internal/aliases"
	"github.com/JohnnyVBut/cascade/internal/firewall"
	"github.com/JohnnyVBut/cascade/internal/nat"
)

// RegisterAliases registers all /api/aliases/* routes.
func RegisterAliases(api fiber.Router) {
	g := api.Group("/aliases")

	g.Get("", listAliases)
	g.Post("", createAlias)

	g.Get("/:id", getAlias)
	g.Patch("/:id", updateAlias)
	g.Delete("/:id", deleteAlias)

	g.Post("/:id/upload", uploadAlias)
	g.Post("/:id/generate", generateAlias)
	g.Get("/:id/generate/:jobId", getAliasJobStatus)
}

// GET /api/aliases
func listAliases(c *fiber.Ctx) error {
	list, err := aliases.Get().GetAll()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(list)
}

// GET /api/aliases/:id
func getAlias(c *fiber.Ctx) error {
	a, err := aliases.Get().GetByID(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	if a == nil {
		return fiber.NewError(fiber.StatusNotFound, "alias not found")
	}
	return c.JSON(a)
}

// POST /api/aliases
// Body: Alias { name, type, entries?, description?, generatorOpts? }
func createAlias(c *fiber.Ctx) error {
	var inp aliases.Alias
	if err := c.BodyParser(&inp); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}
	a, err := aliases.Get().Create(inp)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	return c.Status(fiber.StatusCreated).JSON(a)
}

// PATCH /api/aliases/:id
func updateAlias(c *fiber.Ctx) error {
	var upd aliases.Alias
	if err := c.BodyParser(&upd); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}
	a, err := aliases.Get().Update(c.Params("id"), upd)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	// Re-apply NAT and Firewall rules when non-ipset alias changes.
	// ipset aliases update atomically via `ipset swap` — no re-apply needed.
	// For host/network/group/port/port-group: iptables rules are expanded per-entry
	// and must be replaced to pick up added or removed entries.
	if a.Type != "ipset" {
		reapplyAliasRules()
	}
	return c.JSON(a)
}

// reapplyAliasRules flushes and re-applies all NAT and Firewall rules.
// Called after a non-ipset alias is modified so that CIDR-expanded iptables
// rules are replaced with the current alias content.
func reapplyAliasRules() {
	go func() {
		nat.Get().ReapplyAll()
		if err := firewall.Get().RebuildChains(); err != nil {
			log.Printf("aliases: reapplyAliasRules: firewall rebuild: %v", err)
		}
	}()
}

// DELETE /api/aliases/:id
func deleteAlias(c *fiber.Ctx) error {
	if err := aliases.Get().Delete(c.Params("id")); err != nil {
		return fiber.NewError(fiber.StatusNotFound, err.Error())
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// POST /api/aliases/:id/upload
// Body: { text: string } — raw text content with one CIDR per line.
// Lines starting with '#' and empty lines are ignored.
// Writes valid entries to a temp file and calls UploadFromFile.
func uploadAlias(c *fiber.Ctx) error {
	var body struct {
		Text string `json:"text"`
	}
	if err := c.BodyParser(&body); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body: expected { text: string }")
	}
	if strings.TrimSpace(body.Text) == "" {
		return fiber.NewError(fiber.StatusBadRequest, "file is empty")
	}

	// Parse lines: skip empty lines and comments (#).
	var entries []string
	for _, line := range strings.Split(body.Text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		entries = append(entries, line)
	}
	if len(entries) == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "no valid CIDR entries found in file")
	}

	// Write to a temp file; UploadFromFile reads from disk.
	tmpFile, err := os.CreateTemp("", "awg-alias-upload-*.txt")
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to create temp file")
	}
	defer os.Remove(tmpFile.Name())

	for _, entry := range entries {
		if _, err := fmt.Fprintln(tmpFile, entry); err != nil {
			tmpFile.Close()
			return fiber.NewError(fiber.StatusInternalServerError, "failed to write temp file")
		}
	}
	tmpFile.Close()

	a, err := aliases.Get().UploadFromFile(c.Params("id"), tmpFile.Name())
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	return c.JSON(a)
}

// POST /api/aliases/:id/generate
// Body: GeneratorOpts { source, country?, asn?, asnList? }
// Starts an async generation job and returns { jobId } immediately.
// Poll GET /generate/:jobId for completion.
func generateAlias(c *fiber.Ctx) error {
	var opts aliases.GeneratorOpts
	if err := c.BodyParser(&opts); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}
	// StartGenerate is non-blocking; it returns the job ID and launches a goroutine.
	jobID, err := aliases.Get().StartGenerate(c.Params("id"), opts)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	return c.JSON(fiber.Map{"jobId": jobID})
}

// GET /api/aliases/:id/generate/:jobId
// Returns the current status of an async generation job.
// Response: { status: "running"|"done"|"error"|"unknown", entryCount?, error? }
// The frontend polls this every 3s until status == "done" or "error",
// then calls loadAliases() to refresh the prefix count.
//
// When status is "done" this handler eagerly writes entryCount to the DB
// (FinalizeGeneration) before responding, so that the subsequent loadAliases()
// call from the frontend always sees the updated count.
// This fixes the race condition where watchJob's 2s sleep can arrive after
// the frontend's 3s poll, causing loadAliases() to read entryCount=0.
func getAliasJobStatus(c *fiber.Ctx) error {
	aliasID := c.Params("id")
	jobID := c.Params("jobId")
	status := aliases.Get().GetJobStatus(jobID)
	if status.Status == "done" && status.EntryCount > 0 {
		aliases.Get().FinalizeGeneration(aliasID, status.EntryCount)
	}
	return c.JSON(status)
}
