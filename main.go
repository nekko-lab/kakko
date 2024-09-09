package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	driverMysql "github.com/go-sql-driver/mysql"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/template/django/v3"
	"github.com/google/uuid"
	"github.com/sivchari/gotwtr"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func init() {
	const location = "Asia/Tokyo"

	loc, err := time.LoadLocation(location)
	if err != nil {
		loc = time.FixedZone(location, 9*60*60)
	}

	time.Local = loc
}

var db *gorm.DB

type DBConfig struct {
	Username string
	Password string
	Host     string
	Port     int
	Database string
}

func InitDB(c *DBConfig) (err error) {
	db, err = gorm.Open(mysql.New(mysql.Config{
		DSNConfig: &driverMysql.Config{
			User:                 c.Username,
			Passwd:               c.Password,
			Net:                  "tcp",
			Addr:                 fmt.Sprintf("%s:%d", c.Host, c.Port),
			DBName:               c.Database,
			Collation:            "utf8mb4_general_ci",
			ParseTime:            true,
			AllowNativePasswords: true,
		},
	}))
	if err != nil {
		return
	}

	db.Logger = db.Logger.LogMode(logger.Info)

	return db.AutoMigrate(&PostRequest{})
}

type PostRequest struct {
	ID        uuid.UUID `gorm:"type:char(36);not null;primaryKey"`
	Content   string    `gorm:"type:text;not null"`
	CreatedAt time.Time `gorm:"type:datetime;not null;default:CURRENT_TIMESTAMP"`
}

func (pr *PostRequest) TableName() string {
	return "post_requests"
}

func getenv(key, fallback string) string {
	value := os.Getenv(key)
	if len(value) == 0 {
		return fallback
	}

	return value
}

//go:embed pages
var pages embed.FS

var (
	confirmPassword = getenv("CONFIRM_PASSWORD", "password")
	webhookURL      = getenv("WEBHOOK_URL", "")
	xKey            = getenv("X_KEY", "")
	dev             = getenv("DEV", "true") == "true"
)

func main() {
	port, err := strconv.Atoi(getenv("DB_PORT", "3306"))
	if err != nil {
		log.Panic(err)
	}

	c := &DBConfig{
		Username: getenv("DB_USERNAME", "root"),
		Password: getenv("DB_PASSWORD", "password"),
		Host:     getenv("DB_HOST", "localhost"),
		Port:     port,
		Database: getenv("DB_DATABASE", "kakko"),
	}
	err = InitDB(c)
	if err != nil {
		log.Panic(err)
	}

	engine := django.NewPathForwardingFileSystem(http.FS(pages), "/pages", ".django")

	app := fiber.New(fiber.Config{
		Views: engine,
	})

	app.Get("/", getPost)
	app.Post("/", postPost)
	app.Get("/reviews/:id", getReview)
	app.Post("/reviews/:id", postReview)

	log.Panic(app.Listen(":9000"))
}

func getPost(c *fiber.Ctx) error {
	return c.Render("post", fiber.Map{}, "layout")
}

func postPost(c *fiber.Ctx) error {
	content := c.FormValue("content")

	id, err := uuid.NewV7()
	if err != nil {
		return c.Status(http.StatusInternalServerError).SendString(err.Error())
	}

	pr := &PostRequest{
		ID:        id,
		Content:   content,
		CreatedAt: time.Now(),
	}

	err = db.Create(pr).Error
	if err != nil {
		return c.Status(http.StatusInternalServerError).SendString(err.Error())
	}

	err = sendDiscordWebhook(`## 新しいポストリクエストが作成されました

ポストリクエストを承認してください。
http://localhost:9000/reviews/` + pr.ID.String())
	if err != nil {
		return c.Status(http.StatusInternalServerError).SendString(err.Error())
	}

	return c.Render("post_confirm", fiber.Map{
		"ID":      pr.ID,
		"Content": content,
	}, "layout")
}

func getReview(c *fiber.Ctx) error {
	id := c.Params("id")

	var pr PostRequest
	err := db.Where("id = ?", id).First(&pr).Error
	if err != nil {
		return c.Status(http.StatusInternalServerError).SendString(err.Error())
	}

	return c.Render("review", fiber.Map{
		"ID":      pr.ID,
		"Content": pr.Content,
	}, "layout")
}

func postReview(c *fiber.Ctx) error {
	password := c.FormValue("password")
	if password != confirmPassword {
		return c.Status(http.StatusUnauthorized).SendString("Unauthorized")
	}

	id := c.Params("id")

	var pr PostRequest
	err := db.Where("id = ?", id).First(&pr).Error
	if err != nil {
		return c.Status(http.StatusInternalServerError).SendString(err.Error())
	}

	if dev {
		err = sendDiscordWebhook("## ポスト送信テスト\n\n```\n" + pr.Content + "\n```")
		if err != nil {
			return c.Status(http.StatusInternalServerError).SendString(err.Error())
		}
	} else {
		err = sendPost(c.Context(), pr.Content)
		if err != nil {
			return c.Status(http.StatusInternalServerError).SendString(err.Error())
		}
	}

	err = db.Delete(&pr).Error
	if err != nil {
		return c.Status(http.StatusInternalServerError).SendString(err.Error())
	}

	return c.Render("review_confirm", fiber.Map{
		"ID":      pr.ID,
		"Content": pr.Content,
	}, "layout")
}

func sendDiscordWebhook(content string) error {
	contentJson, err := json.Marshal(struct {
		Content string `json:"content"`
	}{
		Content: content,
	})
	if err != nil {
		return err
	}

	_, err = http.Post(
		webhookURL,
		"application/json",
		bytes.NewBuffer(contentJson),
	)
	if err != nil {
		return err
	}

	return nil
}

func sendPost(ctx context.Context, content string) error {
	client := gotwtr.New(xKey)
	_, err := client.PostTweet(ctx, &gotwtr.PostTweetOption{
		Text: content,
	})
	if err != nil {
		return err
	}

	return nil
}
