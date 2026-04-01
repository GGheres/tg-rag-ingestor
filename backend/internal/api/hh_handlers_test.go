package api

import "testing"

func TestResolveOptionalBool(t *testing.T) {
	trueValue := true
	falseValue := false

	tests := []struct {
		name     string
		value    *bool
		fallback bool
		want     bool
	}{
		{name: "nil uses fallback true", value: nil, fallback: true, want: true},
		{name: "nil uses fallback false", value: nil, fallback: false, want: false},
		{name: "explicit true overrides fallback", value: &trueValue, fallback: false, want: true},
		{name: "explicit false overrides fallback", value: &falseValue, fallback: true, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveOptionalBool(tt.value, tt.fallback)
			if got != tt.want {
				t.Fatalf("resolveOptionalBool() = %v, want %v", got, tt.want)
			}
		})
	}
}
