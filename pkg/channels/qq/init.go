package qq

import (
	"github.com/zhazhaku/reef/pkg/bus"
	"github.com/zhazhaku/reef/pkg/channels"
	"github.com/zhazhaku/reef/pkg/config"
)

func init() {
	channels.RegisterFactory(
		config.ChannelQQ,
		func(channelName, channelType string, cfg *config.Config, b *bus.MessageBus) (channels.Channel, error) {
			bc := cfg.Channels[channelName]
			decoded, err := bc.GetDecoded()
			if err != nil {
				return nil, err
			}
			c, ok := decoded.(*config.QQSettings)
			if !ok {
				return nil, channels.ErrSendFailed
			}
			return NewQQChannel(bc, c, b)
		},
	)
}
