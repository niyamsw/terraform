package terraform

import (
	"testing"

	"github.com/hashicorp/terraform/internal/configs"
	"github.com/hashicorp/terraform/internal/plans"
)

func TestTriggersForEvent(t *testing.T) {
	// Setup a resource node with a mix of before and after create and update events.
	node := &NodeAbstractResourceInstance{
		// Just enough NodeAbstractResourceInstance for the triggersForEvent function
		NodeAbstractResource: NodeAbstractResource{
			Config: &configs.Resource{
				Managed: &configs.ManagedResource{
					ActionTriggers: []*configs.ActionTrigger{
						{
							Events: []configs.ActionTriggerEvent{configs.BeforeCreate, configs.AfterCreate},
							Actions: []configs.ActionRef{
								{ConfigAction: mustActionAddr("action.provider_example.create").Config()},
							},
						},
						{
							Events: []configs.ActionTriggerEvent{configs.BeforeUpdate, configs.AfterUpdate},
							Actions: []configs.ActionRef{
								{ConfigAction: mustActionAddr("action.provider_example.create").Config()},
							},
						},
						{
							Events: []configs.ActionTriggerEvent{configs.BeforeUpdate},
							Actions: []configs.ActionRef{
								{ConfigAction: mustActionAddr("action.provider_example.create").Config()},
							},
						},
						{
							Events: []configs.ActionTriggerEvent{configs.BeforeUpdate},
							Actions: []configs.ActionRef{
								{ConfigAction: mustActionAddr("action.provider_example.create").Config()},
							},
						},
					},
				},
			},
		},
	}

	// triggersForEvent copies the entire action trigger (which does not compare
	// well), so we're not bothering to confirm every field; just that we got
	// the expected triggers.
	n := NodePlannableResourceInstance{NodeAbstractResourceInstance: node}
	gotB, gotA := n.triggersForEvent(plans.Create)
	if len(gotB) != 1 {
		t.Fatal("wrong results")
	}

	var seenBefore, seenAfter bool
	for _, event := range gotB[0].Events {
		switch event {
		case configs.BeforeCreate:
			seenBefore = true
		case configs.AfterCreate:
			seenAfter = true
		default:
			t.Fatalf("unexpected event in results from action = plans.Create")
		}
	}

	if !seenBefore || !seenAfter {
		t.Fatal("wrong results")
	}

	if gotA[0] != gotB[0] {
		t.Fatal("the create action should have returned the same action trigger (twice)")
	}

	gotB, gotA = n.triggersForEvent(plans.Update) // 2 triggers include before_update (only 1 after_update)
	if len(gotA) != 1 && len(gotB) != 2 {
		t.Fatal("wrong number of update triggers")
	}

	// once delete is implemented this will be able to include delete events
	gotB, gotA = n.triggersForEvent(plans.CreateThenDelete)
	if len(gotA) != 1 && len(gotB) != 1 {
		t.Fatal("createThenDelete did not return the correct number of (create) events") // This test will fail once delete is implemented!
	}

	gotB, gotA = n.triggersForEvent(plans.Delete) // this one too!
	if len(gotA) != 0 && len(gotB) != 0 {
		t.Fatal("how did we even get a delete action?")
	}
}
