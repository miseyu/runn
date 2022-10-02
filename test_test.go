package runn

import (
	"context"
	"errors"
	"testing"
)

func TestTestRun(t *testing.T) {
	tests := []struct {
		cond    string
		runned  bool
		wantErr interface{}
	}{
		{"vars.foo.bar == 'baz'", false, nil},
		{"vars.foo.bar == 'xxx'", false, &condFalseError{}},
		{"steps[0].res.status == 403", false, nil},
		{"current.res.status == 403", false, &condFalseError{}},
		{"current.res.status == 403", true, nil},
	}
	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.cond, func(t *testing.T) {
			o, err := New(Var("foo", map[string]interface{}{
				"bar": "baz",
			}))
			o.store.steps = []map[string]interface{}{
				{
					"res": map[string]interface{}{
						"status": 403,
					},
				},
			}
			if err != nil {
				t.Fatal(err)
			}
			r, err := newTestRunner(o)
			if err != nil {
				t.Fatal(err)
			}
			if err := r.Run(ctx, tt.cond, tt.runned); err != nil {
				if !errors.As(err, &tt.wantErr) {
					t.Errorf("got %v\nwant %v", err, tt.wantErr)
				}
				return
			}
			if tt.wantErr != nil {
				t.Error("want error\n")
			}
		})
	}
}
