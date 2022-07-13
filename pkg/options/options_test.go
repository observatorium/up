package options

import (
	"fmt"
	"testing"

	"github.com/efficientgo/tools/core/pkg/testutil"
	"github.com/prometheus/prometheus/prompb"
)

func TestLabelArg_Sort(t *testing.T) {
	testCases := []struct {
		original labelArg
		expected labelArg
	}{
		{
			// Test sorting with lower-case label names (__name__ should be first).
			labelArg{
				prompb.Label{Name: "z", Value: "1"},
				prompb.Label{Name: "a", Value: "1"},
				prompb.Label{Name: "__name__", Value: "test"},
			},
			labelArg{
				prompb.Label{Name: "__name__", Value: "test"},
				prompb.Label{Name: "a", Value: "1"},
				prompb.Label{Name: "z", Value: "1"},
			},
		},
		{
			// Test sorting with upper-case label names (upper case should be first).
			labelArg{
				prompb.Label{Name: "__name__", Value: "test"},
				prompb.Label{Name: "A", Value: "1"},
				prompb.Label{Name: "b", Value: "1"},
			},
			labelArg{
				prompb.Label{Name: "A", Value: "1"},
				prompb.Label{Name: "__name__", Value: "test"},
				prompb.Label{Name: "b", Value: "1"},
			},
		},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("case #%d", i), func(t *testing.T) {
			tc.original.Sort()
			testutil.Equals(t, tc.expected, tc.original)
		})
	}
}
