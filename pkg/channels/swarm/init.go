package swarm

import (
	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/channels"
	"github.com/sipeed/picoclaw/pkg/config"
)

func init() {
	channels.RegisterSafeFactory(config.ChannelSwarm,
		func(bc *config.Channel, settings *config.SwarmSettings, b *bus.MessageBus) (channels.Channel, error) {
			return NewSwarmChannel(bc, settings, b)
		})
}
