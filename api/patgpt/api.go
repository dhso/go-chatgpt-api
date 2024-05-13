package patgpt

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
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

type StreamingChoice struct {
	Delta        Message `json:"delta"`
	FinishReason string  `json:"finish_reason"`
	Index        int     `json:"index"`
}

type Message struct {
	Role         string        `json:"role"`
	Content      any           `json:"content"`
	ToolCalls    []interface{} `json:"tool_calls"`
	Name         string        `json:"name"`
	FunctionCall interface{}   `json:"function_call"`
	ToolCallId   string        `json:"tool_call_id"`
}

type MessageContent struct {
	Type     string               `json:"type"`
	ImageUrl ImageUrl             `json:"image_url,omitempty"`
	Text     string               `json:"text,omitempty"`
	Source   MessageContentSource `json:"source,omitempty"`
}

type ImageUrl struct {
	Url string `json:"url"`
}
type MessageContentSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type FormattedResp struct {
	Model  string                 `json:"model"`
	Choice string                 `json:"choice"`
	Usage  map[string]interface{} `json:"usage"`
}

type OpenAISubscriptionResponse struct {
	Object             string  `json:"object"`
	HasPaymentMethod   bool    `json:"has_payment_method"`
	SoftLimitUSD       float64 `json:"soft_limit_usd"`
	HardLimitUSD       float64 `json:"hard_limit_usd"`
	SystemHardLimitUSD float64 `json:"system_hard_limit_usd"`
	AccessUntil        int64   `json:"access_until"`
}

type OpenAIRequest struct {
	Stream      bool          `json:"stream"`
	Model       string        `json:"model"`
	MaxToken    int           `json:"max_tokens"`
	Message     string        `json:"message"`
	Messages    []Message     `json:"messages"`
	Temperature float64       `json:"temperature"`
	Tools       []interface{} `json:"tools"`
	ToolChoice  any           `json:"tool_choice"`
}

type OpenAIEmbeddingRequest struct {
	Input          any    `json:"input"`
	Model          string `json:"model"`
	EncodingFormat string `json:"encoding_format"`
}

type OpenAIUsageResponse struct {
	Object string `json:"object"`
	//DailyCosts []OpenAIUsageDailyCost `json:"daily_costs"`
	TotalUsage float64 `json:"total_usage"` // unit: 0.01 dollar
}

func CreateCompletions(c *gin.Context) {
	CreateChatCompletions(c)
}

func CreateChatCompletions(c *gin.Context) {
	reqBody, _ := io.ReadAll(c.Request.Body)
	var request OpenAIRequest
	json.Unmarshal(reqBody, &request)

	url := HandleUrl(c, request)
	body := HandleBody(c, request, reqBody)
	resp, err := HandlePost(c, url, body, request)
	if err != nil {
		return
	}

	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		HandleResponse(c, resp, request)
		return
	case http.StatusUnauthorized:
		logger.Error(fmt.Sprintf(api.AccountDeactivatedErrorMessage, c.GetString(c.Request.Header.Get(api.AuthorizationHeader))))
	case http.StatusForbidden:
		logger.Error(fmt.Sprintf(api.AccountForbiddenErrorMessage, c.GetString(c.Request.Header.Get(api.AuthorizationHeader))))
	}
	responseMap := make(map[string]interface{})
	json.NewDecoder(resp.Body).Decode(&responseMap)
	c.AbortWithStatusJSON(resp.StatusCode, responseMap)
}

func HandleUrl(c *gin.Context, request OpenAIRequest) string {
	return getPatApiUrlPrefix() + patApiCreateCompletions
}

func HandleBody(c *gin.Context, request OpenAIRequest, body []byte) []byte {
	if len(request.Messages) == 0 {
		return body
	}

	for i, message := range request.Messages {
		if message.Role == "system" && (strings.HasPrefix(request.Model, "claude-") || strings.HasPrefix(request.Model, "gemini-")) {
			message.Role = "user"
			request.Messages[i] = message
		}
		switch contents := message.Content.(type) {
		case string:
			continue
		case []interface{}:
			// 循环处理messages
			for j, content := range contents {
				_content := content.(map[string]interface{})
				if _content["type"] == "image_url" {
					base64Str := _content["image_url"].(map[string]interface{})["url"].(string)
					if !strings.HasPrefix(base64Str, "data:") {
						// 访问图片链接转成base64
						base64Str = api.GetImageBase64Str(base64Str)
					}
					base64Parts := strings.Split(base64Str, ";")
					if len(base64Parts) < 2 {
						continue
					}
					mediaType := strings.TrimPrefix(base64Parts[0], "data:")
					data := strings.TrimPrefix(base64Parts[1], "base64,")
					content = map[string]interface{}{
						"type": "image",
						"source": map[string]interface{}{
							"type":       "base64",
							"media_type": mediaType,
							"data":       data,
						},
					}
					request.Messages[i].Content.([]interface{})[j] = content
				}
			}
		}

	}

	if len(request.Messages) >= 2 && request.Messages[0].Role == "user" && request.Messages[1].Role == "user" {
		request.Messages = append(request.Messages[:2], request.Messages[1:]...)
		request.Messages[1] = Message{
			Role:    "assistant",
			Content: "ok",
		}
	}

	newBody, err := json.Marshal(request)
	if err != nil {
		return body
	}
	return newBody
}

func HandleResponse(c *gin.Context, resp *http.Response, request OpenAIRequest) {
	if request.Stream {
		HandleCompletionsResponseWithStream(c, resp)
	} else {
		HandleCompletionsResponse(c, resp)
	}
}

func HandleClaudeResponseWithStream(c *gin.Context, resp *http.Response) {
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

		if strings.HasPrefix(line, "event:") || line == "" {
			continue
		}

		if strings.HasPrefix(line, "data:") {
			line = "data: " + strings.TrimPrefix(line, "data:")
		}

		var jsonLine map[string]interface{}
		err = json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &jsonLine)
		if err == nil {
			model := jsonLine["model"].(string)
			if model == "" {
				continue
			}
			var delta Message
			var finishReason string
			if jsonLine["message"] != nil {
				content := jsonLine["message"].(string)
				delta = Message{
					Role:    "assistant",
					Content: content,
				}
			} else {
				finishReason = "stop"
			}

			var choices []StreamingChoice
			choices = append(choices, StreamingChoice{
				Delta:        delta,
				FinishReason: finishReason,
				Index:        0,
			})
			usage := map[string]interface{}{}
			var ts = time.Now().Unix()
			id := "chatcmpl-" + fmt.Sprint(ts)
			strLine, err := json.Marshal(map[string]interface{}{
				"model":   model,
				"choices": choices,
				"usage":   usage,
				"id":      id,
				"object":  "chat.completion",
				"created": ts,
			})
			if err != nil {
				break
			}

			line = "data: " + string(strLine)
		}

		if line == "data: finish" {
			line = "data: [DONE]"
		}

		c.Writer.Write([]byte(line + "\n\n"))
		c.Writer.Flush()
	}
}

func HandleCompletionsResponseWithStream(c *gin.Context, resp *http.Response) {
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

		if strings.HasPrefix(line, "event:") || line == "" {
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

func HandleCompletionsResponse(c *gin.Context, resp *http.Response) {
	responseMap := make(map[string]interface{})
	json.NewDecoder(resp.Body).Decode(&responseMap)
	errorCode := responseMap["error_code"].(float64)
	if errorCode == 429 {
		errorMessage := responseMap["msg"].(string)
		c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
			"error": gin.H{
				"message": errorMessage,
				"type":    "rate_limit_error",
				"param":   "",
				"code":    "invalid_api_key",
			},
		})
		return
	}
	if responseMap["data"] == nil && responseMap["msg"] != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, api.ReturnMessage(responseMap["msg"].(string)))
		return
	}

	jsonData := responseMap["data"].(map[string]interface{})
	finishReason := jsonData["finish_reason"]
	if finishReason == nil {
		finishReason = ""
	}
	switch v := finishReason.(type) {
	case float64:
		finishReason = strconv.FormatFloat(v, 'f', -1, 64)
	case string:
		finishReason = v
	default:
		finishReason = ""
	}
	var choices []Choice
	choices = append(choices, Choice{
		Message: Message{
			Role: "assistant",
		},
		FinishReason: finishReason.(string),
		Index:        0,
	})
	message := jsonData["message"]
	if message != nil {
		choices[0].Message.Content = message.(string)
	}
	toolCalls := jsonData["tool_calls"]
	if toolCalls != nil {
		choices[0].Message.ToolCalls = toolCalls.([]interface{})
	}
	model := jsonData["model"].(string)
	usage := jsonData["usage"].(map[string]interface{})
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

func HandlePost(c *gin.Context, url string, data []byte, request OpenAIRequest) (*http.Response, error) {
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(data))
	req.Header.Set(api.AuthorizationHeader, api.GetBasicToken(c))
	if strings.HasPrefix(request.Model, "gpt-") {
		if request.Model == "gpt-4-turbo" {
			req.Header.Set("X-Ai-Engine", "openai")
		} else {
			req.Header.Set("X-Ai-Engine", "azure")
		}
	} else if strings.HasPrefix(request.Model, "claude-") {
		req.Header.Set("X-Ai-Engine", "anthropic")
	} else if strings.HasPrefix(request.Model, "gemini-") {
		req.Header.Set("X-Ai-Engine", "google")
	} else if strings.HasPrefix(request.Model, "patent-") {
		req.Header.Set("X-Ai-Engine", "patsnap")
	}
	if request.Stream {
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

func GetBillingSubscription(c *gin.Context) {
	url := getPatApiUrlPrefix() + patApiCostUsage
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set(api.AuthorizationHeader, api.GetBasicToken(c))
	req.Header.Set("Content-Type", "application/json")
	resp, err := api.Client.Do(req)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, api.ReturnMessage(err.Error()))
		return
	}

	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		responseMap := make(map[string]interface{})
		json.NewDecoder(resp.Body).Decode(&responseMap)
		c.AbortWithStatusJSON(resp.StatusCode, responseMap)
		return
	}
	responseMap := make(map[string]interface{})
	json.NewDecoder(resp.Body).Decode(&responseMap)
	limit := responseMap["data"].(map[string]interface{})["limit"].(float64)
	usage := responseMap["data"].(map[string]interface{})["usage"].(float64)
	userType := responseMap["data"].(map[string]interface{})["type"].(string)
	total := limit + usage
	c.JSON(http.StatusOK, gin.H{
		"object":                userType,
		"has_payment_method":    true,
		"soft_limit_usd":        total,
		"hard_limit_usd":        total,
		"system_hard_limit_usd": total,
		"access_until":          1,
	})
}

func GetBillingUsage(c *gin.Context) {
	url := getPatApiUrlPrefix() + patApiCostUsage
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set(api.AuthorizationHeader, api.GetBasicToken(c))
	req.Header.Set("Content-Type", "application/json")
	resp, err := api.Client.Do(req)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, api.ReturnMessage(err.Error()))
		return
	}

	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		responseMap := make(map[string]interface{})
		json.NewDecoder(resp.Body).Decode(&responseMap)
		c.AbortWithStatusJSON(resp.StatusCode, responseMap)
		return
	}
	responseMap := make(map[string]interface{})
	json.NewDecoder(resp.Body).Decode(&responseMap)
	usage := responseMap["data"].(map[string]interface{})["usage"].(float64)
	userType := responseMap["data"].(map[string]interface{})["type"].(string)
	c.JSON(http.StatusOK, gin.H{
		"object":      userType,
		"total_usage": usage * 100,
	})
}

func CreateEmbeddings(c *gin.Context) {
	reqBody, _ := io.ReadAll(c.Request.Body)
	var request OpenAIEmbeddingRequest
	json.Unmarshal(reqBody, &request)
	url := getPatApiUrlPrefix() + patApiCreateEmbeddings
	results := make(map[string]interface{})
	switch inputs := request.Input.(type) {
	case string:
		req, _ := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(reqBody))
		results = doEmbeddingsRequest(c, req)
	case []interface{}:
		// 循环处理messages
		embeddings := make([]map[string]interface{}, len(inputs))
		var wg sync.WaitGroup
		mu := sync.Mutex{}
		for i, input := range inputs {
			wg.Add(1)
			go func(_i int, _input interface{}) {
				defer wg.Done()
				_request := request
				_request.Input = _input
				_reqBody, _ := json.Marshal(_request)
				req, _ := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(_reqBody))
				result := doEmbeddingsRequest(c, req)
				mu.Lock()
				embedding := result["data"].([]interface{})[0].(map[string]interface{})
				embedding["index"] = _i
				embeddings[_i] = embedding
				results = map[string]interface{}{
					"object": result["object"],
					"model":  result["model"],
					"data":   embeddings,
					"usage":  result["usage"].(map[string]interface{}),
				}
				mu.Unlock()
			}(i, input)
		}
		wg.Wait()
	}
	c.JSON(http.StatusOK, results)
}

func doEmbeddingsRequest(c *gin.Context, req *http.Request) map[string]interface{} {
	req.Header.Set(api.AuthorizationHeader, api.GetBasicToken(c))
	req.Header.Set("X-Ai-Engine", "openai")
	req.Header.Set("Content-Type", "application/json")
	resp, err := api.Client.Do(req)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, api.ReturnMessage(err.Error()))
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		logger.Error(fmt.Sprintf(api.AccountDeactivatedErrorMessage, c.GetString(c.Request.Header.Get(api.AuthorizationHeader))))
		responseMap := make(map[string]interface{})
		json.NewDecoder(resp.Body).Decode(&responseMap)
		c.AbortWithStatusJSON(resp.StatusCode, responseMap)
		return nil
	}
	responseMap := make(map[string]interface{})
	json.NewDecoder(resp.Body).Decode(&responseMap)
	jsonData := responseMap["data"].(map[string]interface{})
	return jsonData
}
