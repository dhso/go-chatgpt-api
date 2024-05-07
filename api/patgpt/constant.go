package patgpt

import (
	"encoding/base64"
	"os"
)

func decoded(code string) string {
	decodedCode, err := base64.StdEncoding.DecodeString(code)
	if err != nil {
		return ""
	}
	return string(decodedCode)
}

const (
	patApiUrlPrefix             = "aHR0cHM6Ly9wYXQtYXBpLm1pbndzLmNvbQ=="
	patApiCreateChatCompletions = "/compute/openai_chatgpt_turbo"
	patApiCreateCompletions     = "/compute/openai_chatgpt_turbo"
	patApiAggregation           = "/compute/chatgpt_aggregation"
	patApiCostUsage             = "/common/cost/usage"
	patApiCreateEmbeddings      = "/compute/openai_embeddings"
)

func getPatApiUrlPrefix() string {
	if os.Getenv("PAT_URL") != "" {
		return os.Getenv("PAT_URL")
	}
	return decoded(patApiUrlPrefix)
}
