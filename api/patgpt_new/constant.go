package patgpt_new

import (
	"encoding/base64"
	"os"
)

const (
	patApiUrlPrefix             = "aHR0cHM6Ly9wYXQtYXBpLm1pbndzLmNvbQ=="
	patApiCreateChatCompletions = "/v1/chat/completions"
	patApiCreateCompletions     = "/v1/completions"
	patApiCreateEmbeddings      = "/v1/embeddings"
	patApiCostUsage             = "/common/cost/usage"
)

func decoded(code string) string {
	decodedCode, err := base64.StdEncoding.DecodeString(code)
	if err != nil {
		return ""
	}
	return string(decodedCode)
}

func getPatApiUrlPrefix() string {
	if os.Getenv("PAT_URL") != "" {
		return os.Getenv("PAT_URL")
	}
	return decoded(patApiUrlPrefix)
}
