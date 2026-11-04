// Package usage provides read-only provider usage snapshots.
package usage

import (
	"context"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/global"
	service "github.com/JieWaZi/NeuxBot/server/services/usage"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx"
)

// Router registers usage summaries and grouped pages.
var Router = ginx.NewRouter(ginx.Group("/usage"))

func init() {
	Router.Register(&GetUsageSummary{})
	Router.Register(&ListUsageGroups{})
}

func filter(from, to int64, timezone, wakerIDs, chatID, provider, accountID, source string) service.Filter {
	var wakers []string
	if wakerIDs != "" {
		wakers = strings.Split(wakerIDs, ",")
	}
	return service.Filter{From: from, To: to, Timezone: timezone, WakerIDs: wakers,
		ChatID: chatID, Provider: provider, AccountID: accountID, Source: source}
}

// GetUsageSummary queries the provider usage overview.
type GetUsageSummary struct {
	// MethodGet declares the read-only HTTP method.
	ginx.MethodGet
	// From 是包含的 Unix 毫秒下界。
	From int64 `in:"query" name:"from" validate:"required,min=1"`
	// To 是排除的 Unix 毫秒上界。
	To int64 `in:"query" name:"to" validate:"required,min=1"`
	// Timezone 是 IANA 时区。
	Timezone string `in:"query" name:"timezone" validate:"required,max=80"`
	// WakerIDs 是逗号分隔的原始伙伴标识。
	WakerIDs string `in:"query" name:"wakerIds" validate:"max=12900"`
	// ChatID 是原始对话标识。
	ChatID string `in:"query" name:"chatId" validate:"max=128"`
	// Provider 是原生后端标识。
	Provider string `in:"query" name:"provider" validate:"max=32"`
	// AccountID 是原始宿主账号标识。
	AccountID string `in:"query" name:"accountId" validate:"max=128"`
	// Source 是原始调用入口。
	Source string `in:"query" name:"source" validate:"max=32"`
	// Snapshot 是概要返回的不透明快照。
	Snapshot string `in:"query" name:"snapshot" validate:"max=2048"`
}

func (*GetUsageSummary) Path() string { return "/summary" }

func (d *GetUsageSummary) Output(ctx *gin.Context) (interface{}, error) {
	request, cancel := context.WithTimeout(ctx.Request.Context(), 20*time.Second)
	defer cancel()
	result, err := global.UsageService.Summary(request, filter(d.From, d.To, d.Timezone, d.WakerIDs, d.ChatID, d.Provider, d.AccountID, d.Source), d.Snapshot)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return result, nil
}

// ListUsageGroups paginates the same provider usage snapshot.
type ListUsageGroups struct {
	// MethodGet declares the read-only HTTP method.
	ginx.MethodGet
	// From 是包含的 Unix 毫秒下界。
	From int64 `in:"query" name:"from" validate:"required,min=1"`
	// To 是排除的 Unix 毫秒上界。
	To int64 `in:"query" name:"to" validate:"required,min=1"`
	// Timezone 是 IANA 时区。
	Timezone string `in:"query" name:"timezone" validate:"required,max=80"`
	// WakerIDs 是逗号分隔的原始伙伴标识。
	WakerIDs string `in:"query" name:"wakerIds" validate:"max=12900"`
	// ChatID 是原始对话标识。
	ChatID string `in:"query" name:"chatId" validate:"max=128"`
	// Provider 是原生后端标识。
	Provider string `in:"query" name:"provider" validate:"max=32"`
	// AccountID 是原始宿主账号标识。
	AccountID string `in:"query" name:"accountId" validate:"max=128"`
	// Source 是原始调用入口。
	Source string `in:"query" name:"source" validate:"max=32"`
	// Snapshot 是概要返回的不透明快照。
	Snapshot string `in:"query" name:"snapshot" validate:"required,max=2048"`
	// GroupBy 是分组维度。
	GroupBy string `in:"query" name:"groupBy" validate:"required,oneof=day model waker provider"`
	// Limit 是页大小，零使用默认值 50。
	Limit int `in:"query" name:"limit" validate:"min=0,max=100"`
	// Cursor 是同一快照的下一页游标。
	Cursor string `in:"query" name:"cursor" validate:"max=2048"`
}

func (*ListUsageGroups) Path() string { return "/groups" }

func (d *ListUsageGroups) Output(ctx *gin.Context) (interface{}, error) {
	request, cancel := context.WithTimeout(ctx.Request.Context(), 20*time.Second)
	defer cancel()
	result, err := global.UsageService.Groups(request, filter(d.From, d.To, d.Timezone, d.WakerIDs, d.ChatID, d.Provider, d.AccountID, d.Source), d.Snapshot, d.GroupBy, d.Cursor, d.Limit)
	if err != nil {
		return nil, ginx.WithStack(err)
	}
	return result, nil
}
