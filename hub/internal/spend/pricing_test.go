package spend

import (
	"strings"
	"testing"
)

// #53: priceFor takes the first matching prefix, so no row may be a prefix
// of a row after it, whatever order prices.json lists them in.
func TestNoPricePrefixShadowsALaterOne(t *testing.T) {
	for i, a := range prices {
		for _, b := range prices[i+1:] {
			if strings.HasPrefix(b.Prefix, a.Prefix) {
				t.Errorf("%q comes before %q and would swallow it", a.Prefix, b.Prefix)
			}
		}
	}
}
