package main

import (
	tp "github.com/enfein/mieru/v3/apis/trafficpattern"
	"google.golang.org/protobuf/proto"
	"testing"
)

func TestPatternsChangeOnlyFragmentation(t *testing.T) {
	a, b := pattern(false), pattern(true)
	for _, p := range []bool{false, true} {
		decoded, err := tp.Decode(tp.Encode(pattern(p)))
		if err != nil {
			t.Fatal(err)
		}
		if err := tp.Validate(decoded); err != nil {
			t.Fatal(err)
		}
		if !proto.Equal(decoded, pattern(p)) {
			t.Fatal("pattern did not survive serialization")
		}
	}
	b.TcpFragment.Enable = proto.Bool(false)
	if !proto.Equal(a, b) {
		t.Fatal("A/B changes more than TCP fragmentation")
	}
}
