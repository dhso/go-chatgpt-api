package chatgpt

import (
	"fmt"
	"os"
	"time"

	"github.com/PuerkitoBio/goquery"
	http "github.com/bogdanfinn/fhttp"

	"github.com/dhso/go-chatgpt-api/api"
	"github.com/linweiyuan/go-logger/logger"
)

const (
	healthCheckUrl         = "https://chat.openai.com/backend-api/accounts/check"
	errorHintBlock         = "looks like you have bean blocked by OpenAI, please change to a new IP or have a try with WARP"
	errorHintFailedToStart = "check OpenAI failed: %s"
	healthCheckPass        = "OpenAI check passed"
	sleepHours             = 8760 // 365 days
)

func init() {
	proxyUrl := os.Getenv("PROXY")
	if proxyUrl != "" {
		logger.Info("PROXY: " + proxyUrl)
		api.Client.SetProxy(proxyUrl)
		// wait for proxy to be ready
		time.Sleep(time.Second)
	}

	resp := healthCheck()
	checkHealthCheckStatus(resp)
	logger.Info(api.ReadyHint)
}

func healthCheck() (resp *http.Response) {
	req, _ := http.NewRequest(http.MethodGet, healthCheckUrl, nil)
	req.Header.Set("User-Agent", api.UserAgent)
	resp, err := api.Client.Do(req)
	if err != nil {
		logger.Warn("failed to health check: " + err.Error())
	}
	return resp
}

func checkHealthCheckStatus(resp *http.Response) {
	if resp != nil {
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusUnauthorized {
			logger.Info(healthCheckPass)
		} else {
			doc, _ := goquery.NewDocumentFromReader(resp.Body)
			alert := doc.Find(".message").Text()
			if alert != "" {
				logger.Warn(errorHintBlock)
			} else {
				logger.Warn(fmt.Sprintf(errorHintFailedToStart, resp.Status))
			}
		}
	}
}
