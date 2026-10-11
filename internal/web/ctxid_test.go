package web

import "testing"

func TestEncodeDecodeCtxRoundTrip(t *testing.T) {
	names := []string{
		"kind-kui-test",
		"arn:aws:eks:us-east-1:744839990696:cluster/it-aws-platform",
		"",
	}
	for _, name := range names {
		id := encodeCtx(name)
		got, err := decodeCtx(id)
		if err != nil {
			t.Fatalf("decodeCtx(%q) for name %q: %v", id, name, err)
		}
		if got != name {
			t.Errorf("round trip mismatch: encodeCtx(%q) -> %q -> decodeCtx -> %q", name, id, got)
		}
	}
}

func TestEncodeCtxHasNoURLUnsafeChars(t *testing.T) {
	id := encodeCtx("arn:aws:eks:us-east-1:744839990696:cluster/it-aws-platform")
	for _, c := range id {
		if c == '/' || c == ':' {
			t.Errorf("encoded context id %q still contains a path-breaking character %q", id, c)
		}
	}
}

func TestDecodeCtxInvalid(t *testing.T) {
	if _, err := decodeCtx("not valid base64url!!"); err == nil {
		t.Error("expected an error decoding invalid base64url input")
	}
}
