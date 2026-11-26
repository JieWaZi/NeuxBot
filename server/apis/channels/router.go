// Package channels 提供团队配置、团队对话和任务编排接口。
package channels

import "github.com/shrewx/ginx"

// Router 是团队接口路由。
var Router = ginx.NewRouter(ginx.Group("/channels"))

func init() {
	Router.Register(&ListChannels{})
	Router.Register(&CreateChannel{})
	Router.Register(&GetChannel{})
	Router.Register(&UpdateChannel{})
	Router.Register(&DeleteChannel{})
	Router.Register(&ListChannelThreads{})
	Router.Register(&CreateChannelThread{})
	Router.Register(&GetChannelThread{})
	Router.Register(&DeleteChannelThread{})
	Router.Register(&ListChannelTimeline{})
	Router.Register(&UpdateChannelThreadMemberConfigs{})
	Router.Register(&SendChannelMessage{})
	Router.Register(&CreateChannelTask{})
	Router.Register(&FollowUpChannelTask{})
	Router.Register(&ReviewChannelTask{})
	Router.Register(&StopChannelTask{})
	Router.Register(&StopChannelWork{})
	Router.Register(&StopFirstChannelWork{})
	Router.Register(&ListChannelEvents{})
	Router.Register(&ListChannelLeadInputs{})
}
