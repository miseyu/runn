package runn

import (
	"context"
	"fmt"
)

const bindRunnerKey = "bind"

type bindRunner struct {
	operator *operator
}

func newBindRunner(o *operator) (*bindRunner, error) {
	return &bindRunner{
		operator: o,
	}, nil
}

func (rnr *bindRunner) Run(ctx context.Context, cond map[string]string) error {
	store := rnr.operator.store.toMap()
	store[storeIncludedKey] = rnr.operator.included
	store[storePreviousKey] = rnr.operator.store.previous()
	store[storeCurrentKey] = rnr.operator.store.latest()
	for k, v := range cond {
		if k == storeVarsKey || k == storeStepsKey || k == storeParentKey || k == storeIncludedKey || k == storeCurrentKey || k == storePreviousKey || k == loopCountVarKey {
			return fmt.Errorf("'%s' is reserved", k)
		}
		vv, err := Eval(v, store)
		if err != nil {
			return err
		}
		rnr.operator.store.bindVars[k] = vv
	}
	return nil
}
