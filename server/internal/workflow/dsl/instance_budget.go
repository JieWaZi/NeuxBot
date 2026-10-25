package dsl

import (
	"fmt"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// ValidateInstanceBudget 拒绝连最少正常执行路径都无法容纳的实例预算。
func ValidateInstanceBudget(bundle ProgramBundle, limits Limits) error {
	minimum := minimumInstances(bundle.Programs[bundle.Entry])
	if limits.MaxInstances > 0 && limits.MaxInstances < minimum {
		return status_error.WorkflowBudgetExceeded.WithField("budget", constants.WorkflowInstanceBudget).WithField("reason", instanceBudgetReason(limits.MaxInstances, minimum))
	}
	return nil
}

func instanceBudgetReason(limit, minimum int) string {
	return fmt.Sprintf("maxInstances=%d cannot complete this workflow; at least %d node instances are required; increase maxInstances or omit it to use the host limit", limit, minimum)
}

// minimumInstances 沿控制流求正常返回的最少效果数，不把互斥分支、可跳过循环或组内动态项相加。
func minimumInstances(program Program) int {
	if len(program.Code) == 0 {
		return 0
	}
	for _, instruction := range program.Code {
		// 异常可能进入 catch 或 finally 并提前返回；尚不能证明其下界时不拒绝源码。
		if instruction.Op == constants.WorkflowOpTry {
			return 0
		}
	}
	unreachable := len(program.Code) + 1
	distance := make([]int, len(program.Code))
	for index := range distance {
		distance[index] = unreachable
	}
	distance[0] = 0
	queue := []int{0}
	minimum := unreachable
	for index := 0; index < len(queue); index++ {
		pc := queue[index]
		instruction := program.Code[pc]
		count := distance[pc]
		if instruction.Op == constants.WorkflowOpEffect && instruction.Primitive != "phase" && instruction.Primitive != "log" && instruction.Primitive != "parallel" && instruction.Primitive != "pipeline" {
			count++
		}
		if instruction.Op == constants.WorkflowOpReturn {
			minimum = min(minimum, count)
			continue
		}
		if instruction.Op == constants.WorkflowOpThrow {
			continue
		}
		next := []int{pc + 1}
		switch instruction.Op {
		case constants.WorkflowOpBranch:
			next = []int{instruction.Next, instruction.Alternate}
		case constants.WorkflowOpJump, constants.WorkflowOpLoopControl:
			next = []int{instruction.Next}
		}
		for _, target := range next {
			if target >= 0 && target < len(distance) && count < distance[target] {
				distance[target] = count
				queue = append(queue, target)
			}
		}
	}
	if minimum == unreachable {
		return 0
	}
	return minimum
}
