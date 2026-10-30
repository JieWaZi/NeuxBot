package runtimehost

import (
	"errors"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	runtimeconfig "github.com/JieWaZi/NeuxBot/server/internal/agent/environment/config"
	runtimecontract "github.com/JieWaZi/NeuxBot/server/internal/agent/host/contract"
	"testing"
)

func TestPrepareForkMCPServersRejectsDuplicateRuntimeKey(t *testing.T) {
	_, err := PrepareForkMCPServers([]runtimecontract.Connector{
		{ID: "first", Name: "第一个", RuntimeKey: "ally_shared_a1b2c3d4", Transport: runtimeconfig.ConnectorTransportStdio, Command: "first"},
		{ID: "second", Name: "第二个", RuntimeKey: "ally_shared_a1b2c3d4", Transport: runtimeconfig.ConnectorTransportStdio, Command: "second"},
	})
	if err == nil || !errors.Is(err, status_error.BadRequest) {
		t.Fatalf("duplicate runtime key error = %v", err)
	}
}
