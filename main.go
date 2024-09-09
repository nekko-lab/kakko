package main

import (
	"embed"
	"log"
	"net/http"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/template/django/v3"
)

//go:embed pages
var pages embed.FS

func main() {
	engine := django.NewPathForwardingFileSystem(http.FS(pages), "/pages", ".django")

	app := fiber.New(fiber.Config{
		Views: engine,
	})

	app.Get("/", renderPostPage)
	app.Post("/", postPost)

	log.Panic(app.Listen(":9000"))
}

func renderPostPage(c *fiber.Ctx) error {
	return c.Render("post", fiber.Map{}, "layout")
}

func postPost(c *fiber.Ctx) error {
	content := c.FormValue("content")

	return c.Render("confirm", fiber.Map{
		"Content": content,
	}, "layout")
}
