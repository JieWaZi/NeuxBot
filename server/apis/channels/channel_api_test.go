package channels

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

// channelConfigExecutionFake 仅提供团队配置写入所需的统一执行配置校验。
type channelConfigExecutionFake struct {
	// Service 补齐本测试不使用的执行接口方法。
	execution.Service
}

// ValidateConfiguration 接受本用例构造的 Runtime 配置。
func (*channelConfigExecutionFake) ValidateConfiguration(
	context.Context,
	execution.ExecutionConfiguration,
) error {
	return nil
}

// apiTestTimestamp 返回团队 API 测试模型使用的可选毫秒时间。
func apiTestTimestamp(value int64) *int64 { return &value }

func TestChannelAPICreatesConfigurationWithoutChats(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{
		Type:    conf.Sqlite,
		DBName:  filepath.Join(t.TempDir(), "channel-api.db"),
		Migrate: true,
	})
	if err != nil {
		t.Fatalf("create test database: %v", err)
	}
	sqlDB, err := database.DB.DB()
	if err != nil {
		t.Fatalf("get sql database: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := sqlDB.Close(); closeErr != nil {
			t.Errorf("close test database: %v", closeErr)
		}
	})

	previousWakers := global.WakerControllerManager
	previousChannels := global.ChannelControllerManager
	previousTransactions := global.TransactionControllerManager
	previousExecutionService := global.ExecutionService
	t.Cleanup(func() {
		global.WakerControllerManager = previousWakers
		global.ChannelControllerManager = previousChannels
		global.TransactionControllerManager = previousTransactions
		global.ExecutionService = previousExecutionService
	})
	global.WakerControllerManager = controllers.NewWakerController(database.DB)
	global.ChannelControllerManager = controllers.NewChannelController(database.DB)
	global.TransactionControllerManager = controllers.NewTransactionController(
		dbhelper.NewGormTransactionManager(database.DB),
	)
	global.ExecutionService = &channelConfigExecutionFake{}

	ctx, _ := gin.CreateTestContext(nil)
	now := time.Now().UnixMilli()
	lead := models.Waker{
		ID:          util.New(),
		Name:        "负责人",
		RoleName:    "架构师",
		Avatar:      "avatar:lead",
		Bio:         "负责协调",
		Expertise:   nil,
		WorkStyle:   nil,
		WorkMethods: nil,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	member := models.Waker{
		ID:          util.New(),
		Name:        "成员",
		RoleName:    "开发者",
		Avatar:      "avatar:member",
		Bio:         "负责执行",
		Expertise:   nil,
		WorkStyle:   nil,
		WorkMethods: nil,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := global.WakerControllerManager.Create(ctx, lead); err != nil {
		t.Fatalf("create lead: %v", err)
	}
	if err := global.WakerControllerManager.Create(ctx, member); err != nil {
		t.Fatalf("create member: %v", err)
	}

	create := CreateChannel{Body: ChannelInput{
		Name:        " 交付团队 ",
		Description: " 团队说明 ",
		LeadWakerID: lead.ID,
		Members: []ChannelMemberInput{
			{
				WakerID:         member.ID,
				RuntimeID:       "codex",
				AccountID:       "system",
				Model:           "gpt-5.6",
				Reasoning:       "high",
				PermissionLevel: constants.PermissionLevelAuto,
			},
			{
				WakerID:         lead.ID,
				RuntimeID:       "claude",
				Model:           "sonnet",
				Reasoning:       "",
				PermissionLevel: constants.PermissionLevelDefault,
			},
		},
	}}
	if err := create.Validate(ctx); err != nil {
		t.Fatalf("validate create: %v", err)
	}
	createdValue, err := create.Output(ctx)
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	created := createdValue.(ChannelDetail)
	if created.Channel.Name != "交付团队" || created.Channel.LeadWakerID != lead.ID {
		t.Fatalf("unexpected channel: %+v", created.Channel)
	}
	if len(created.Members) != 2 || created.Members[0].AccountID != "system" || created.Members[0].Waker.ID != member.ID || created.Members[1].Waker.ID != lead.ID {
		t.Fatalf("unexpected ordered members: %+v", created.Members)
	}
	var chatCount int64
	if err := database.DB.Model(&models.Chat{}).Count(&chatCount).Error; err != nil {
		t.Fatalf("count chats: %v", err)
	}
	var threadCount int64
	if err := database.DB.Model(&models.ChannelThread{}).Count(&threadCount).Error; err != nil {
		t.Fatalf("count threads: %v", err)
	}
	if chatCount != 0 || threadCount != 0 {
		t.Fatalf("create channel produced chats=%d threads=%d", chatCount, threadCount)
	}

}

func TestChannelInputRequiresTwoDistinctMembersAndExplicitLead(t *testing.T) {
	ctx, _ := gin.CreateTestContext(nil)
	base := ChannelMemberInput{
		WakerID:         "member-one",
		RuntimeID:       "codex",
		PermissionLevel: constants.PermissionLevelDefault,
	}
	tests := []struct {
		// name 是子测试名称。
		name string
		// input 是待校验团队配置。
		input ChannelInput
		// expected 是期望返回的稳定状态错误。
		expected error
	}{
		{
			name: "one member",
			input: ChannelInput{
				Name:        "团队",
				LeadWakerID: base.WakerID,
				Members:     []ChannelMemberInput{base},
			},
			expected: status_error.ChannelMemberCountInvalid,
		},
		{
			name: "duplicate members",
			input: ChannelInput{
				Name:        "团队",
				LeadWakerID: base.WakerID,
				Members:     []ChannelMemberInput{base, base},
			},
			expected: status_error.ChannelMemberCountInvalid,
		},
		{
			name: "lead outside members",
			input: ChannelInput{
				Name:        "团队",
				LeadWakerID: "missing-lead",
				Members: []ChannelMemberInput{
					base,
					{
						WakerID:         "member-two",
						RuntimeID:       "codex",
						PermissionLevel: constants.PermissionLevelDefault,
					},
				},
			},
			expected: status_error.ChannelLeadInvalid,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := CreateChannel{Body: test.input}
			if err := request.Validate(ctx); !errors.Is(err, test.expected) {
				t.Fatalf("Validate() error = %v, want %v", err, test.expected)
			}
		})
	}
}
