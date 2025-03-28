package api

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	http "github.com/bogdanfinn/fhttp"
	tls_client "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
	"github.com/gin-gonic/gin"
	"github.com/xqdoo00o/OpenAIAuth/auth"
	"github.com/xqdoo00o/funcaptcha"

	"github.com/linweiyuan/go-logger/logger"
)

const (
	ChatGPTApiPrefix    = "/chatgpt"
	ImitateApiPrefix    = "/imitate/v1"
	ChatGPTApiUrlPrefix = "https://chat.openai.com"

	PlatformApiPrefix    = "/platform"
	PlatformApiUrlPrefix = "https://api.openai.com"

	defaultErrorMessageKey             = "errorMessage"
	AuthorizationHeader                = "Authorization"
	XAuthorizationHeader               = "X-Authorization"
	ContentType                        = "application/x-www-form-urlencoded"
	UserAgent                          = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36 Edg/124.0.0.0"
	Auth0Url                           = "https://auth0.openai.com"
	LoginUsernameUrl                   = Auth0Url + "/u/login/identifier?state="
	LoginPasswordUrl                   = Auth0Url + "/u/login/password?state="
	ParseUserInfoErrorMessage          = "failed to parse user login info"
	GetAuthorizedUrlErrorMessage       = "failed to get authorized url"
	EmailInvalidErrorMessage           = "email is not valid"
	EmailOrPasswordInvalidErrorMessage = "email or password is not correct"
	GetAccessTokenErrorMessage         = "failed to get access token"
	defaultTimeoutSeconds              = 600 // 10 minutes

	EmailKey                       = "email"
	AccountDeactivatedErrorMessage = "account %s is deactivated"
	AccountForbiddenErrorMessage   = "account %s is forbidden"

	ReadyHint = "service go-chatgpt-api is ready"

	refreshPuidErrorMessage = "failed to refresh PUID"

	Version = "2024.06.18.1"
)

var (
	Client       tls_client.HttpClient
	ArkoseClient tls_client.HttpClient
	PUID         string
	ProxyUrl     string
)

type LoginInfo struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type AuthLogin interface {
	GetAuthorizedUrl(csrfToken string) (string, int, error)
	GetState(authorizedUrl string) (string, int, error)
	CheckUsername(state string, username string) (int, error)
	CheckPassword(state string, username string, password string) (string, int, error)
	GetAccessToken(code string) (string, int, error)
	GetAccessTokenFromHeader(c *gin.Context) (string, int, error)
}

func init() {
	Client, _ = tls_client.NewHttpClient(tls_client.NewNoopLogger(), []tls_client.HttpClientOption{
		tls_client.WithCookieJar(tls_client.NewCookieJar()),
		tls_client.WithTimeoutSeconds(defaultTimeoutSeconds),
		tls_client.WithClientProfile(profiles.Okhttp4Android13),
	}...)
	ArkoseClient = getHttpClient()

	setupPUID()
}

func NewHttpClient() tls_client.HttpClient {
	client := getHttpClient()

	ProxyUrl = os.Getenv("PROXY")
	if ProxyUrl != "" {
		client.SetProxy(ProxyUrl)
	}

	return client
}

func getHttpClient() tls_client.HttpClient {
	client, _ := tls_client.NewHttpClient(tls_client.NewNoopLogger(), []tls_client.HttpClientOption{
		tls_client.WithCookieJar(tls_client.NewCookieJar()),
		tls_client.WithClientProfile(profiles.Okhttp4Android13),
	}...)
	return client
}

func Proxy(c *gin.Context) {
	url := c.Request.URL.Path
	if strings.Contains(url, ChatGPTApiPrefix) {
		url = strings.ReplaceAll(url, ChatGPTApiPrefix, ChatGPTApiUrlPrefix)
	} else if strings.Contains(url, ImitateApiPrefix) {
		url = strings.ReplaceAll(url, ImitateApiPrefix, ChatGPTApiUrlPrefix+"/backend-api")
	} else {
		url = strings.ReplaceAll(url, PlatformApiPrefix, PlatformApiUrlPrefix)
	}

	method := c.Request.Method
	queryParams := c.Request.URL.Query().Encode()
	if queryParams != "" {
		url += "?" + queryParams
	}

	// if not set, will return 404
	c.Status(http.StatusOK)

	var req *http.Request
	if method == http.MethodGet {
		req, _ = http.NewRequest(http.MethodGet, url, nil)
	} else {
		body, _ := io.ReadAll(c.Request.Body)
		req, _ = http.NewRequest(method, url, bytes.NewReader(body))
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set(AuthorizationHeader, GetAccessToken(c))
	resp, err := Client.Do(req)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, ReturnMessage(err.Error()))
		return
	}

	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusUnauthorized {
			logger.Error(fmt.Sprintf(AccountDeactivatedErrorMessage, c.GetString(EmailKey)))
		}

		responseMap := make(map[string]interface{})
		json.NewDecoder(resp.Body).Decode(&responseMap)
		c.AbortWithStatusJSON(resp.StatusCode, responseMap)
		return
	}

	io.Copy(c.Writer, resp.Body)
}

func ReturnMessage(msg string) gin.H {
	logger.Warn(msg)

	return gin.H{
		defaultErrorMessageKey: msg,
	}
}

func GetAccessToken(c *gin.Context) string {
	accessToken := c.GetString(AuthorizationHeader)
	if !strings.HasPrefix(accessToken, "Bearer") {
		return "Bearer " + accessToken
	}

	return accessToken
}

func GetArkoseToken() (string, error) {
	return funcaptcha.GetOpenAIToken(PUID, ProxyUrl)
}

func GetBearerRemovedToken(c *gin.Context) string {
	accessToken := c.GetString(AuthorizationHeader)
	if strings.HasPrefix(accessToken, "Bearer") {
		accessToken = strings.Replace(accessToken, "Bearer", "", 1)
	}
	return strings.TrimSpace(accessToken)
}

func GetBearerToken(c *gin.Context) string {
	accessToken := c.GetString(AuthorizationHeader)
	if !strings.HasPrefix(accessToken, "Bearer") {
		return "Bearer " + accessToken
	}
	return accessToken
}

func GetBasicToken(c *gin.Context) string {
	basicToken := c.GetString(AuthorizationHeader)
	if strings.HasPrefix(basicToken, "Bearer") {
		basicToken = strings.Replace(basicToken, "Bearer", "Basic", 1)
	}
	if !strings.HasPrefix(basicToken, "Basic") {
		return "Basic " + basicToken
	}

	return basicToken
}

func GetGithubToken(c *gin.Context) string {
	token := c.GetString(AuthorizationHeader)
	if strings.HasPrefix(token, "Bearer") {
		token = strings.Replace(token, "Bearer", "token", 1)
	}
	if !strings.HasPrefix(token, "token") {
		return "token " + token
	}

	return token
}

func setupPUID() {
	username := os.Getenv("OPENAI_EMAIL")
	password := os.Getenv("OPENAI_PASSWORD")
	if username != "" && password != "" {
		go func() {
			for {
				authenticator := auth.NewAuthenticator(username, password, ProxyUrl)
				if err := authenticator.Begin(); err != nil {
					logger.Warn(fmt.Sprintf("%s: %s", refreshPuidErrorMessage, err.Details))
					return
				}

				accessToken := authenticator.GetAccessToken()
				if accessToken == "" {
					logger.Error(refreshPuidErrorMessage)
					return
				}

				puid, err := authenticator.GetPUID()
				if err != nil {
					logger.Error(refreshPuidErrorMessage)
					return
				}

				PUID = puid

				time.Sleep(time.Hour * 24 * 7)
			}
		}()
	}
}

func GetImageBase64Str(url string) string {
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	resp, err := Client.Do(req)
	if err != nil {
		logger.Error(err.Error())
		return ""
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Error(err.Error())
		return ""
	}
	base64Str := base64.StdEncoding.EncodeToString(body)
	format := ""
	if strings.HasPrefix(base64Str, "/9j") {
		format = "image/jpg"
	} else if strings.HasPrefix(base64Str, "iVB") {
		format = "image/png"
	} else if strings.HasPrefix(base64Str, "R0l") {
		format = "image/gif"
	} else if strings.HasPrefix(base64Str, "Ukl") {
		format = "image/webp"
	} else if strings.HasPrefix(base64Str, "Qk0") {
		format = "image/bmp"
	} else if strings.HasPrefix(base64Str, "JVB") {
		format = "image/pdf"
	}
	base64Str = "data:" + format + ";base64," + base64Str
	return base64Str
}

func ModelMappping(model string) string {
	if model == "gpt-4o" || model == "gpt-3.5-turbo" || model == "gpt-4" {
		return "azure"
	} else if strings.HasPrefix(model, "claude-") {
		return "anthropic"
	} else if strings.HasPrefix(model, "gemini-") {
		return "google"
	} else if strings.HasPrefix(model, "patent-") || strings.HasPrefix(model, "seekgpt-") {
		return "patsnap"
	} else if strings.HasPrefix(model, "deepseek-") {
		return "deepseek"
	}
	return "openai"
}
