package minimax

import (
	"fmt"

	channelconstant "github.com/sundaqiang/new-api/constant"
	relaycommon "github.com/sundaqiang/new-api/relay/common"
	"github.com/sundaqiang/new-api/relay/constant"
)

func GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	baseUrl := info.ChannelBaseUrl
	if baseUrl == "" {
		baseUrl = channelconstant.ChannelBaseURLs[channelconstant.ChannelTypeMiniMax]
	}

	switch info.RelayMode {
	case constant.RelayModeChatCompletions:
		return fmt.Sprintf("%s/v1/text/chatcompletion_v2", baseUrl), nil
	case constant.RelayModeAudioSpeech:
		return fmt.Sprintf("%s/v1/t2a_v2", baseUrl), nil
	default:
		return "", fmt.Errorf("unsupported relay mode: %d", info.RelayMode)
	}
}
