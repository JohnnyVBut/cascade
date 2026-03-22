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
	"os"

	"github.com/gofiber/fiber/v2"

	"github.com/JohnnyVBut/cascade/internal/aliases"
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
	return c.JSON(a)
}

// DELETE /api/aliases/:id
func deleteAlias(c *fiber.Ctx) error {
	if err := aliases.Get().Delete(c.Params("id")); err != nil {
		return fiber.NewError(fiber.StatusNotFound, err.Error())
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// POST /api/aliases/:id/upload
// Body: JSON array of CIDR strings (uploaded prefix list).
// Writes entries to a temp file and calls UploadFromFile (which expects a file path).
func uploadAlias(c *fiber.Ctx) error {
	var entries []string
	if err := c.BodyParser(&entries); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body: expected array of strings")
	}

	// Write entries to a temporary file; UploadFromFile reads from disk.
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
