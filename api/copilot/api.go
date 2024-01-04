package copilot

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	http "github.com/bogdanfinn/fhttp"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/dhso/go-chatgpt-api/api"
	"github.com/linweiyuan/go-logger/logger"
)

// type CachedTokens map[string]CachedToken

type CachedToken struct {
	token      string
	fetched_at time.Time
}

var (
	cached_tokens map[string]CachedToken = make(map[string]CachedToken)
	machineId     string
	once          sync.Once
)

func CreateChatCompletions(c *gin.Context) {
	body, _ := io.ReadAll(c.Request.Body)
	var request struct {
		Stream bool `json:"stream"`
	}
	json.Unmarshal(body, &request)

	url := copilotChatCompletionsApi

	resp, err := handlePost(c, url, body, request.Stream)
	if err != nil {
		return
	}

	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		logger.Error(fmt.Sprintf(api.AccountDeactivatedErrorMessage, c.GetString(c.Request.Header.Get(api.AuthorizationHeader))))
		responseMap := make(map[string]interface{})
		json.NewDecoder(resp.Body).Decode(&responseMap)
		c.AbortWithStatusJSON(resp.StatusCode, responseMap)
		return
	}

	if request.Stream {
		io.Copy(c.Writer, resp.Body)
	} else {
		io.Copy(c.Writer, resp.Body)
	}
}

func CreateCompletions(c *gin.Context) {
	CreateChatCompletions(c)
}

func handlePost(c *gin.Context, url string, data []byte, stream bool) (*http.Response, error) {
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(data))
	req.Header.Set("Host", copilotApiHost)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(api.AuthorizationHeader, "Bearer "+getToken(c.Request.Header.Get(api.AuthorizationHeader)))
	req.Header.Set("X-Request-Id", uuid.New().String())
	req.Header.Set("X-Github-Api-Version", "2023-07-07")
	req.Header.Set("Vscode-Sessionid", getSessionId())
	req.Header.Set("Vscode-machineid", getMachineId())
	req.Header.Set("Editor-Version", "vscode/1.85.0")
	req.Header.Set("Editor-Plugin-Version", "copilot-chat/0.11.1")
	req.Header.Set("Openai-Organization", "github-copilot")
	req.Header.Set("Openai-Intent", "conversation-panel")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "GitHubCopilotChat/0.11.1")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Encoding", "gzip, deflate, br")

	// if stream {
	// 	req.Header.Set("Accept", "text/event-stream")
	// }
	resp, err := api.Client.Do(req)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, api.ReturnMessage(err.Error()))
		return nil, err
	}
	return resp, nil
}

func getToken(ghu_token string) string {
	ghu_token = strings.TrimSpace(strings.TrimPrefix(ghu_token, "Bearer"))
	value, exists := cached_tokens[ghu_token]
	if exists && time.Since(value.fetched_at) < 15*time.Minute {
		return value.token
	}
	req, _ := http.NewRequest(http.MethodGet, githubCopilotTokenApi, nil)
	req.Header.Set("Host", githubApiHost)
	req.Header.Set("authorization", "token "+ghu_token)
	req.Header.Set("editor-version", "vscode/1.85.0")
	req.Header.Set("editor-plugin-version", "copilot-chat/0.11.1")
	req.Header.Set("user-agent", "GitHubCopilotChat/0.11.1")
	req.Header.Set("accept", "*/*")
	resp, err := api.Client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	responseMap := make(map[string]interface{})
	json.NewDecoder(resp.Body).Decode(&responseMap)
	cached_tokens[ghu_token] = CachedToken{
		token:      responseMap["token"].(string),
		fetched_at: time.Now(),
	}
	return responseMap["token"].(string)
}

func getSessionId() string {
	myUuid := uuid.New().String()
	now := time.Now()
	timestamp := now.UnixNano() / 1e6
	return myUuid + strconv.FormatInt(timestamp, 10)
}

func getMachineId() string {
	once.Do(func() {
		myUuid := uuid.New().String()
		hasher := sha256.New()
		hasher.Write([]byte(myUuid))
		result := hasher.Sum(nil)
		machineId = hex.EncodeToString(result)
	})

	return machineId
}
