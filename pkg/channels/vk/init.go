package vk

import (
	"github.com/zhazhaku/reef/pkg/bus"
	"github.com/zhazhaku/reef/pkg/channels"
	"github.com/zhazhaku/reef/pkg/config"
)

func init() {
	channels.RegisterFactory(
		config.ChannelVK,
		func(channelName, channelType string, cfg *config.Config, b *bus.MessageBus) (channels.Channel, error) {
			bc := cfg.Channels[channelName]
			if bc == nil {
				return nil, channels.ErrSendFailed
			}
			return NewVKChannel(channelName, bc, b)
		},
	)
}
