package status_error

import (
	"testing"

	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/i18nx"
)

func TestChatHasActiveExecutionLocalizesInChinese(t *testing.T) {
	i18nx.Load(&conf.I18N{Langs: []string{"zh"}})
	message := ChatHasActiveExecution.Localize(i18nx.Instance(), "zh")
	if message.Key() != ChatHasActiveExecution.Key() ||
		message.Value() != "当前对话已有运行中的执行" {
		t.Fatalf(
			"localized message = key %q value %q, want key %q value %q",
			message.Key(),
			message.Value(),
			ChatHasActiveExecution.Key(),
			"当前对话已有运行中的执行",
		)
	}
}

func TestRuntimeExecutionFailedLocalizesWithoutDetail(t *testing.T) {
	i18nx.Load(&conf.I18N{Langs: []string{"zh"}})
	message := RuntimeExecutionFailed.Localize(i18nx.Instance(), "zh")
	if message.Value() != "智能体执行失败" {
		t.Fatalf("localized runtime failure = %q", message.Value())
	}
	refused := RuntimeExecutionFailed.WithParams(map[string]interface{}{"ModelUnavailable": true})
	if message := refused.Localize(i18nx.Instance(), "zh"); message.Value() != "当前账号无法使用所选模型" {
		t.Fatalf("localized model refusal = %q", message.Value())
	}
}
