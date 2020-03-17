package runner_test

import (
	"github.com/b2wdigital/restQL-golang/internal/domain"
	"github.com/b2wdigital/restQL-golang/internal/runner"
	"reflect"
	"testing"
)

func TestApplyModifiers(t *testing.T) {
	tests := []struct {
		name      string
		modifiers domain.Modifiers
		resources runner.Resources
		expected  runner.Resources
	}{
		{
			"should do nothing if there is no modifiers",
			domain.Modifiers{},
			runner.Resources{"hero": domain.Statement{Resource: "hero"}},
			runner.Resources{"hero": domain.Statement{Resource: "hero"}},
		},
		{
			"should do nothing if there is no modifiers",
			nil,
			runner.Resources{"hero": domain.Statement{Resource: "hero"}},
			runner.Resources{"hero": domain.Statement{Resource: "hero"}},
		},
		{
			"should apply max-age modifier to statement",
			domain.Modifiers{"max-age": 600},
			runner.Resources{"hero": domain.Statement{Resource: "hero"}},
			runner.Resources{"hero": domain.Statement{
				Resource:     "hero",
				CacheControl: domain.CacheControl{MaxAge: 600},
			}},
		},
		{
			"should not overwrite already define max-age cache qualifier",
			domain.Modifiers{"max-age": 600},
			runner.Resources{"hero": domain.Statement{
				Resource:     "hero",
				CacheControl: domain.CacheControl{MaxAge: 400},
			}},
			runner.Resources{"hero": domain.Statement{
				Resource:     "hero",
				CacheControl: domain.CacheControl{MaxAge: 400},
			}},
		},
		{
			"should apply s-max-age modifier to statement",
			domain.Modifiers{"s-max-age": 600},
			runner.Resources{"hero": domain.Statement{Resource: "hero"}},
			runner.Resources{"hero": domain.Statement{
				Resource:     "hero",
				CacheControl: domain.CacheControl{SMaxAge: 600},
			}},
		},
		{
			"should not overwrite already define smax-age cache qualifier",
			domain.Modifiers{"s-max-age": 600},
			runner.Resources{"hero": domain.Statement{
				Resource:     "hero",
				CacheControl: domain.CacheControl{SMaxAge: 400},
			}},
			runner.Resources{"hero": domain.Statement{
				Resource:     "hero",
				CacheControl: domain.CacheControl{SMaxAge: 400},
			}},
		},
		{
			"should apply cache-control modifier to statement",
			domain.Modifiers{"cache-control": 600},
			runner.Resources{"hero": domain.Statement{Resource: "hero"}},
			runner.Resources{"hero": domain.Statement{
				Resource:     "hero",
				CacheControl: domain.CacheControl{MaxAge: 600},
			}},
		},
		{
			"should not overwrite already define max-age cache qualifier",
			domain.Modifiers{"cache-control": 600},
			runner.Resources{"hero": domain.Statement{
				Resource:     "hero",
				CacheControl: domain.CacheControl{MaxAge: 400},
			}},
			runner.Resources{"hero": domain.Statement{
				Resource:     "hero",
				CacheControl: domain.CacheControl{MaxAge: 400},
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runner.ApplyModifiers(tt.modifiers, tt.resources)

			if !reflect.DeepEqual(got, tt.expected) {
				t.Fatalf("ApplyModifiers = %+#v, want = %+#v", got, tt.expected)
			}
		})
	}
}
