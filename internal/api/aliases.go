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
	"github.com/JohnnyVBut/cascade/internal/tunnel"
)

// RegisterAliases registers all /api/aliases/* routes.
func RegisterAliases(api fiber.Router) {
	g := api.Group("/aliases")

	g.Get("", listAliases)
	g.Post("", createAlias)
	g.Get("/client-groups", listClientGroups) // must be before /:id

	g.Get("/:id", getAlias)
	g.Patch("/:id", updateAlias)
	g.Delete("/:id", deleteAlias)

	g.Post("/:id/upload", uploadAlias)
	g.Get("/:id/entries", getAliasEntries)
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
	aliasID := c.Params("id")

	var upd aliases.Alias
	if err := c.BodyParser(&upd); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}

	// For non-ipset aliases: remove kernel rules BEFORE updating the DB so that
	// buildCmds still resolves the OLD alias content (the entries being removed
	// need to be deleted from the kernel before they disappear from the DB).
	oldAlias, _ := aliases.Get().GetByID(aliasID)
	if oldAlias != nil && oldAlias.Type != "ipset" {
		nat.Get().RemoveForAlias(aliasID)
	}

	oldRateDown, oldRateUp := 0, 0
	if oldAlias != nil && oldAlias.Type == "client-group" {
		oldRateDown, oldRateUp = oldAlias.RateDown, oldAlias.RateUp
	}

	a, err := aliases.Get().Update(aliasID, upd)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}

	// After the DB is updated: re-apply rules using the NEW alias content,
	// then rebuild firewall chains (handles group/port aliases too).
	if a.Type != "ipset" {
		go func() {
			nat.Get().RestoreForAlias(aliasID)
			if err := firewall.Get().RebuildChains(); err != nil {
				log.Printf("aliases: updateAlias: firewall rebuild: %v", err)
			}
		}()
	}

	// Re-apply tc limits for all peers in this group if rate limits changed.
	if a.Type == "client-group" && (a.RateDown != oldRateDown || a.RateUp != oldRateUp) {
		if tm := tunnel.Get(); tm != nil {
			go tm.ApplyGroupTCLimits(aliasID, a.RateDown, a.RateUp)
		}
	}

	return c.JSON(a)
}

// GET /api/aliases/client-groups
// Returns all aliases of type client-group (used by peer create/edit dropdowns).
func listClientGroups(c *fiber.Ctx) error {
	groups, err := aliases.Get().GetClientGroups()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(fiber.Map{"groups": groups})
}

// DELETE /api/aliases/:id
func deleteAlias(c *fiber.Ctx) error {
	// For client-group: returns movedCount for the toast message.
	a, _ := aliases.Get().GetByID(c.Params("id"))
	if a != nil && a.Type == "client-group" {
		moved, err := aliases.Get().DeleteClientGroup(c.Params("id"))
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		}
		return c.JSON(fiber.Map{"movedCount": moved})
	}

	if err := aliases.Get().Delete(c.Params("id")); err != nil {
		return fiber.NewError(fiber.StatusNotFound, err.Error())
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// GET /api/aliases/:id/entries
// Returns current CIDR entries from the kernel ipset.
// Only valid for type=ipset. Used to pre-populate the edit textarea for small sets.
func getAliasEntries(c *fiber.Ctx) error {
	entries, err := aliases.Get().GetIPSetEntries(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	if entries == nil {
		entries = []string{}
	}
	return c.JSON(fiber.Map{"entries": entries})
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
