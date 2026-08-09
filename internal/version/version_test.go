package version

import "testing"

func TestUserAgent(t *testing.T) {
	t.Parallel()

	if got, want := UserAgent(" 1.2.3 "), "terraform-provider-chaptarr/1.2.3"; got != want {
		t.Fatalf("UserAgent = %q, want %q", got, want)
	}
	if got, want := UserAgent(""), "terraform-provider-chaptarr/dev"; got != want {
		t.Fatalf("empty-version UserAgent = %q, want %q", got, want)
	}
}
