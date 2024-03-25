package main

import (
	"log"
	"os"
	"strings"

	http "github.com/bogdanfinn/fhttp"
	"github.com/gin-gonic/gin"

	"github.com/dhso/go-chatgpt-api/api"
	"github.com/dhso/go-chatgpt-api/api/chatgpt"
	"github.com/dhso/go-chatgpt-api/api/copilot"
	"github.com/dhso/go-chatgpt-api/api/imitate"
	"github.com/dhso/go-chatgpt-api/api/patgpt"
	"github.com/dhso/go-chatgpt-api/api/platform"
	_ "github.com/dhso/go-chatgpt-api/env"
	"github.com/dhso/go-chatgpt-api/middleware"
)

func init() {
	gin.ForceConsoleColor()
	gin.SetMode(gin.ReleaseMode)
}

func main() {
	log.Printf("version: %s", "2024032501")
	router := gin.Default()

	router.Use(middleware.CORS())
	router.Use(middleware.Authorization())

	setupChatGPTAPIs(router)
	setupPlatformAPIs(router)
	setupPandoraAPIs(router)
	setupImitateAPIs(router)
	setupPatgptAPIs(router)
	setupCopilotAPIs(router)
	router.NoRoute(api.Proxy)

	router.GET("/", func(c *gin.Context) {
		c.String(http.StatusOK, api.ReadyHint)
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	err := router.Run(":" + port)
	if err != nil {
		log.Fatal("failed to start server: " + err.Error())
	}
}

func setupChatGPTAPIs(router *gin.Engine) {
	chatgptGroup := router.Group("/chatgpt")
	{
		chatgptGroup.POST("/login", chatgpt.Login)
		chatgptGroup.POST("/backend-api/login", chatgpt.Login) // add support for other projects

		conversationGroup := chatgptGroup.Group("/backend-api/conversation")
		{
			conversationGroup.POST("", chatgpt.CreateConversation)
		}
	}
}

func setupPlatformAPIs(router *gin.Engine) {
	platformGroup := router.Group("/platform")
	{
		platformGroup.POST("/login", platform.Login)
		platformGroup.POST("/v1/login", platform.Login)

		apiGroup := platformGroup.Group("/v1")
		{
			apiGroup.POST("/chat/completions", platform.CreateChatCompletions)
			apiGroup.POST("/completions", platform.CreateCompletions)
		}
	}
}

func setupPandoraAPIs(router *gin.Engine) {
	router.Any("/api/*path", func(c *gin.Context) {
		c.Request.URL.Path = strings.ReplaceAll(c.Request.URL.Path, "/api", "/chatgpt/backend-api")
		router.HandleContext(c)
	})
}

func setupImitateAPIs(router *gin.Engine) {
	imitateGroup := router.Group("/imitate")
	{
		imitateGroup.POST("/login", chatgpt.Login)

		apiGroup := imitateGroup.Group("/v1")
		{
			apiGroup.POST("/chat/completions", imitate.CreateChatCompletions)
		}
	}
}

func setupPatgptAPIs(router *gin.Engine) {
	patgptGroup := router.Group("/patgpt")
	{
		apiGroup := patgptGroup.Group("/v1")
		{
			apiGroup.POST("/chat/completions", patgpt.CreateChatCompletions)
			apiGroup.POST("/completions", patgpt.CreateCompletions)
			apiGroup.GET("/dashboard/billing/subscription", patgpt.GetBillingSubscription)
			apiGroup.GET("/dashboard/billing/usage", patgpt.GetBillingUsage)
		}
	}
}

func setupCopilotAPIs(router *gin.Engine) {
	copilotGroup := router.Group("/copilot")
	{
		apiGroup := copilotGroup.Group("/v1")
		{
			apiGroup.POST("/chat/completions", copilot.CreateChatCompletions)
			apiGroup.POST("/completions", copilot.CreateCompletions)
		}
	}
}
