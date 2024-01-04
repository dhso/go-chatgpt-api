package copilot

const (
	copilotApiHost            = "api.githubcopilot.com"
	copilotChatCompletionsApi = "https://" + copilotApiHost + "/chat/completions"
	githubApiHost             = "api.github.com"
	githubCopilotTokenApi     = "https://" + githubApiHost + "/copilot_internal/v2/token"

	getSessionKeyErrorMessage = "failed to get session key"
)
