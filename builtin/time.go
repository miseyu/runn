package builtin

import (
	"time"

	"github.com/araddon/dateparse"
)

func Time(v interface{}) time.Time {
	t, err := dateparse.ParseStrict(v.(string))
	if err != nil {
		return time.Time{}
	}
	return t
}
