package patgpt_new

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	http "github.com/bogdanfinn/fhttp"
	"github.com/gin-gonic/gin"

	"github.com/dhso/go-chatgpt-api/api"
	"github.com/linweiyuan/go-logger/logger"
)

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

type Message struct {
	Role      string        `json:"role"`
	Content   any           `json:"content"`
	ToolCalls []interface{} `json:"tool_calls"`
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
	_body, _ := io.ReadAll(c.Request.Body)
	var request OpenAIRequest
	json.Unmarshal(_body, &request)

	url := getPatApiUrlPrefix() + patApiCreateChatCompletions

	body := HandleBody(c, request, _body)

	resp, err := handlePost(c, url, body, request)
	if err != nil {
		return
	}

	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		switch resp.StatusCode {
		case http.StatusUnauthorized:
			logger.Error(fmt.Sprintf(api.AccountDeactivatedErrorMessage, c.GetString(c.Request.Header.Get(api.AuthorizationHeader))))
		case http.StatusForbidden:
			logger.Error(fmt.Sprintf(api.AccountForbiddenErrorMessage, c.GetString(c.Request.Header.Get(api.AuthorizationHeader))))
		}
		responseMap := make(map[string]interface{})
		json.NewDecoder(resp.Body).Decode(&responseMap)
		c.AbortWithStatusJSON(resp.StatusCode, responseMap)
	} else {
		HandleResponse(c, resp, request)
	}
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

func handlePost(c *gin.Context, url string, data []byte, request OpenAIRequest) (*http.Response, error) {
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(data))
	req.Header.Set(api.AuthorizationHeader, api.GetBearerToken(c))
	if strings.HasPrefix(request.Model, "gpt-") {
		if request.Model == "gpt-4-turbo" || request.Model == "gpt-4o" {
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
	} else if strings.HasPrefix(request.Model, "deepseek-") {
		req.Header.Set("X-Ai-Engine", "deepseek")
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

func HandleResponse(c *gin.Context, resp *http.Response, request OpenAIRequest) {
	if request.Stream {
		c.Writer.Header().Set("Content-Type", "text/event-stream")
		c.Writer.Header().Set("Cache-Control", "no-cache")
		c.Writer.Flush()
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
				line_without_data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				if line_without_data == "finish" {
					line = "data: [DONE]"
				} else {
					line = "data: " + line_without_data
				}
			}

			c.Writer.Write([]byte(line + "\n\n"))
			c.Writer.Flush()
		}
	} else {
		io.Copy(c.Writer, resp.Body)
	}
}

func CreateEmbeddings(c *gin.Context) {
	body, _ := io.ReadAll(c.Request.Body)
	var request OpenAIEmbeddingRequest
	json.Unmarshal(body, &request)

	url := getPatApiUrlPrefix() + patApiCreateEmbeddings

	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(body))
	req.Header.Set(api.AuthorizationHeader, api.GetBearerToken(c))
	req.Header.Set("X-Ai-Engine", "openai")
	resp, err := api.Client.Do(req)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, api.ReturnMessage(err.Error()))
		return
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		io.Copy(c.Writer, resp.Body)
		return
	case http.StatusUnauthorized:
		logger.Error(fmt.Sprintf(api.AccountDeactivatedErrorMessage, c.GetString(c.Request.Header.Get(api.AuthorizationHeader))))
	case http.StatusForbidden:
		logger.Error(fmt.Sprintf(api.AccountForbiddenErrorMessage, c.GetString(c.Request.Header.Get(api.AuthorizationHeader))))
	}
	responseMap := make(map[string]interface{})
	json.NewDecoder(resp.Body).Decode(&responseMap)
	jsonData := responseMap["data"].(map[string]interface{})
	if request.EncodingFormat == "base64" {
		for jd_idx, jd := range jsonData["data"].([]interface{}) {
			// 将浮点数列表转换为字节数组
			buf := new(bytes.Buffer)
			for _, f := range jd.(map[string]interface{})["embedding"].([]interface{}) {
				err := binary.Write(buf, binary.LittleEndian, float32(f.(float64)))
				if err != nil {
					fmt.Println("binary.Write failed:", err)
				}
			}
			byteArray := buf.Bytes()
			// 将字节数组转换为Base64字符串
			base64Str := base64.StdEncoding.EncodeToString(byteArray)
			jsonData["data"].([]interface{})[jd_idx].(map[string]interface{})["embedding"] = base64Str
		}
		c.AbortWithStatusJSON(resp.StatusCode, jsonData)
		return
	}
	c.AbortWithStatusJSON(resp.StatusCode, jsonData)

}

func GetBillingSubscription(c *gin.Context) {
	url := getPatApiUrlPrefix() + patApiCostUsage
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set(api.AuthorizationHeader, api.GetBearerToken(c))
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
	req.Header.Set(api.AuthorizationHeader, api.GetBearerToken(c))
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
