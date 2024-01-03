package patgpt

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	http "github.com/bogdanfinn/fhttp"
	"github.com/gin-gonic/gin"

	"github.com/dhso/go-chatgpt-api/api"
	"github.com/linweiyuan/go-logger/logger"
)

type Choice struct {
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
	Index        int     `json:"index"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type FormattedResp struct {
	Model  string                 `json:"model"`
	Choice string                 `json:"choice"`
	Usage  map[string]interface{} `json:"usage"`
}

func CreateChatCompletions(c *gin.Context) {
	body, _ := io.ReadAll(c.Request.Body)
	var request struct {
		Stream bool `json:"stream"`
	}
	json.Unmarshal(body, &request)

	url := c.Request.URL.Path
	if strings.Contains(url, "/chat") {
		url = decoded(patApiUrlPrefix) + patApiCreateChatCompletions
	} else {
		url = decoded(patApiUrlPrefix) + patApiCreateCompletions
	}

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
		handleCompletionsResponseWithStream(c, resp)
	} else {
		handleCompletionsResponse(c, resp)
	}
}

func CreateCompletions(c *gin.Context) {
	CreateChatCompletions(c)
}

func handleCompletionsResponseWithStream(c *gin.Context, resp *http.Response) {
	c.Writer.Header().Set("Content-Type", "text/event-stream; charset=utf-8")

	reader := bufio.NewReader(resp.Body)
	for {
		if c.Request.Context().Err() != nil {
			break
		}

		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}

		line = strings.TrimSpace(line)

		if strings.HasPrefix(line, "event") || line == "" {
			continue
		}
		if strings.HasPrefix(line, "data:") {
			line = "data: " + strings.TrimPrefix(line, "data:")
		}

		if line == "data: finish" {
			line = "data: [DONE]"
		}

		c.Writer.Write([]byte(line + "\n\n"))
		c.Writer.Flush()
	}
}

func handleCompletionsResponse(c *gin.Context, resp *http.Response) {
	responseMap := make(map[string]interface{})
	json.NewDecoder(resp.Body).Decode(&responseMap)
	var choices []Choice
	choices = append(choices, Choice{
		Message: Message{
			Role:    "assistant",
			Content: responseMap["data"].(map[string]interface{})["message"].(string),
		},
		FinishReason: responseMap["data"].(map[string]interface{})["finish_reason"].(string),
		Index:        0,
	})
	var model string
	model = responseMap["data"].(map[string]interface{})["model"].(string)
	var usage map[string]interface{}
	usage = responseMap["data"].(map[string]interface{})["usage"].(map[string]interface{})
	var ts = time.Now().Unix()
	id := "chatcmpl-" + fmt.Sprint(ts)
	c.JSON(http.StatusOK, gin.H{
		"model":   model,
		"choices": choices,
		"usage":   usage,
		"id":      id,
		"object":  "chat.completion",
		"created": ts,
	})
}

func handlePost(c *gin.Context, url string, data []byte, stream bool) (*http.Response, error) {
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(data))
	req.Header.Set(api.AuthorizationHeader, api.GetBasicToken(c))
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := api.Client.Do(req)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, api.ReturnMessage(err.Error()))
		return nil, err
	}

	return resp, nil
}
