package agent

import (
	"encoding/json"
	"testing"
)

func TestRouteRequestPayload(t *testing.T) {
	cases := []struct {
		name string
		req  routeRequest
		want string
	}{
		{"with model", routeRequest{Text: "hi", Model: "claude-haiku-5-5"}, `{"text":"hi","model":"claude-haiku-5-5"}`},
		// Empty model is omitted so the agent-runner falls back to its own model.
		{"without model", routeRequest{Text: "hi"}, `{"text":"hi"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := json.Marshal(tc.req)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("payload = %s, want %s", got, tc.want)
			}
		})
	}
}
