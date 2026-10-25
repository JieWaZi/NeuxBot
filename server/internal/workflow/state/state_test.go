package state

import (
	"context"
	"strconv"
	"sync"
	"testing"

	workflowdsl "github.com/JieWaZi/NeuxBot/server/internal/workflow/dsl"
)

func TestEffectTimeoutByExecutionType(t *testing.T) {
	manifest := workflowdsl.Manifest{Meta: workflowdsl.Meta{Actions: []workflowdsl.ActionDeclaration{{ID: "command", Type: "script"}, {ID: "request", Type: "http"}}}}
	for _, primitive := range []string{"worker", "functionTask", "command", "request"} {
		for _, explicit := range []bool{false, true} {
			name := primitive
			if explicit {
				name += "-explicit"
			}
			t.Run(name, func(t *testing.T) {
				options := map[string]any{}
				if explicit {
					options["timeoutMs"] = float64(1000)
				}
				boundary := workflowdsl.EffectBoundary{Primitive: primitive, Arguments: []workflowdsl.Value{workflowdsl.JSONValue("prompt"), workflowdsl.JSONValue(options)}}
				expected := int64(600000)
				if primitive == "functionTask" {
					boundary.Arguments = []workflowdsl.Value{workflowdsl.JSONValue(options)}
				}
				if primitive == "command" || primitive == "request" {
					boundary.Primitive = "action"
					boundary.Arguments[0] = workflowdsl.JSONValue(primitive)
				}
				if primitive == "request" {
					expected = 30000
				}
				if explicit {
					expected = 1000
				}
				if primitive == "worker" {
					expected = 0
				}
				if actual := EffectTimeout(manifest, boundary); actual != expected {
					t.Fatalf("timeout=%d expected=%d", actual, expected)
				}
			})
		}
	}
}

func TestEffectDispatchAdmissionSerializesCapacity(t *testing.T) {
	coordinator := New(nil, nil, nil)
	start := make(chan struct{})
	admitted := make(chan string, 64)
	var dispatches sync.WaitGroup
	for index := range 64 {
		dispatches.Add(1)
		go func() {
			defer dispatches.Done()
			<-start
			id := strconv.Itoa(index)
			_, cancel, accepted := coordinator.BeginEffect(context.Background(), id, false, 3)
			if accepted {
				defer cancel()
				admitted <- id
			}
		}()
	}
	close(start)
	dispatches.Wait()
	close(admitted)
	if len(admitted) != 3 {
		t.Fatalf("concurrent dispatches exceeded or lost capacity: %d", len(admitted))
	}
	for id := range admitted {
		if !coordinator.EffectActive(id) {
			t.Fatal("cancelled context released an effect before physical dispatch exit")
		}
		coordinator.EndEffect(id)
	}
	_, cancel, accepted := coordinator.BeginEffect(context.Background(), "after-exit", false, 3)
	if !accepted {
		t.Fatal("physical exits did not release dispatch capacity")
	}
	cancel()
	coordinator.EndEffect("after-exit")
}

func TestEffectCancellationKeepsIdentityUntilDispatchExit(t *testing.T) {
	coordinator := New(nil, nil, nil)
	ctx, cancel, accepted := coordinator.BeginEffect(context.Background(), "effect", false, 3)
	if !accepted {
		t.Fatal("initial dispatch was rejected")
	}
	defer cancel()
	if _, _, accepted := coordinator.BeginEffect(context.Background(), "effect", false, 3); accepted {
		t.Fatal("same effect was dispatched twice")
	}
	if _, _, accepted := coordinator.BeginEffect(context.Background(), "effect", true, 3); accepted {
		t.Fatal("cancellation admitted another dispatch before the old one exited")
	}
	select {
	case <-ctx.Done():
	default:
		t.Fatal("cancellation did not reach the actual registered dispatch")
	}
	if !coordinator.EffectActive("effect") {
		t.Fatal("cancelled dispatch lost its active identity")
	}
	coordinator.EndEffect("effect")
	if coordinator.EffectActive("effect") {
		t.Fatal("exited dispatch remained active")
	}
	_, nextCancel, accepted := coordinator.BeginEffect(context.Background(), "effect", false, 3)
	if !accepted {
		t.Fatal("dispatch remained blocked after physical exit")
	}
	nextCancel()
	coordinator.EndEffect("effect")
}
