package swarm

import (
	"github.com/zhazhaku/reef/pkg/bus"
	"github.com/zhazhaku/reef/pkg/channels"
	"github.com/zhazhaku/reef/pkg/config"
)

func init() {
	channels.RegisterSafeFactory(config.ChannelSwarm,
		func(bc *config.Channel, settings *config.SwarmSettings, b *bus.MessageBus) (channels.Channel, error) {
			return NewSwarmChannel(bc, settings, b)
		})
}
