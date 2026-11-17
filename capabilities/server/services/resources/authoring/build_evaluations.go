package build

import (
	"platformserver/platform"
	"platformserver/services/resources/publication"
)

const SchemaEvaluationSetPublish = TestPlanType + ".publish"

func (b *Build) functionEvaluationEnvironment() publication.FunctionEvaluationEnvironment {
	return publication.FunctionEvaluationEnvironment{
		App: ID, Builder: Builder,
		Authority: func() platform.Authority { return b.ledger },
		Function:  b.functions.Definition,
		Plan: func(c platform.Caller, id string) (publication.FunctionEvaluationPlan, bool) {
			plan, ok := platform.Get[TestPlan](c, id)
			return publication.FunctionEvaluationPlan{Revision: plan.Revision, Function: string(plan.Function), Evaluation: plan.Evaluation}, ok
		},
	}
}
