package api

import "testing"

func TestOpaqueETagMatching(t *testing.T) {
	for _, test := range []struct {
		request, etag string
		match         bool
	}{
		{`W/"a,b"`, `"a,b"`, true}, {`"other", W/"a,b"`, `"a,b"`, true},
		{`"other"`, `"a,b"`, false}, {`"a,b"garbage`, `"a,b"`, false}, {`*`, `"a,b"`, true},
	} {
		if got := matchesETag(test.request, test.etag); got != test.match {
			t.Fatal(test, got)
		}
	}
}
