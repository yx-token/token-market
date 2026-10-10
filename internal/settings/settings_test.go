package settings

import "testing"

func TestProbeTimeoutValidation(t *testing.T) {
	if err := Defaults().Validate(); err != nil {
		t.Fatalf("defaults should be valid: %v", err)
	}
	cases := []struct {
		name           string
		request, batch int
		ok             bool
	}{
		{"defaults", 180, 30, true},
		{"batch equals one request", 600, 10, true},
		{"request too short", 5, 30, false},
		{"request too long", 3600, 120, false},
		{"batch too short", 10, 0, false},
		{"batch too long", 180, 2000, false},
		{"batch shorter than one request", 600, 5, false},
	}
	for _, c := range cases {
		s := Defaults()
		s.ProbeRequestTimeoutSeconds, s.ProbeBatchTimeoutMinutes = c.request, c.batch
		if err := s.Validate(); (err == nil) != c.ok {
			t.Errorf("%s: validate = %v, want ok=%v", c.name, err, c.ok)
		}
	}
}
