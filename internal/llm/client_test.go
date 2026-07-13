package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestClassifyHandlesWebSearchToolCall(t *testing.T) {
	round := 0
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		response := ""
		if r.URL.Host == "search.test" {
			response = `{"message":{"items":[{"DOI":"10.1/test","title":["FAPbI3 result"],"abstract":"evidence","URL":"https://example.test"}]}}`
		} else {
			round++
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if round == 1 {
				response = `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"web_search","arguments":"{\"query\":\"FAPbI3\"}"}}]}}]}`
			} else {
				messages := body["messages"].([]any)
				last := messages[len(messages)-1].(map[string]any)
				if last["role"] != "tool" || last["tool_call_id"] != "call_1" {
					t.Fatalf("missing tool response: %#v", last)
				}
				response = `{"choices":[{"message":{"role":"assistant","content":"{\"is_relevant_perovskite_solar_cell\":true}"}}]}`
			}
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(response))}, nil
	})}
	c := Client{Provider: "deepseek", BaseURL: "https://chat.test", APIKey: "test", Model: "test", HTTP: httpClient, EnableWebSearch: true, WebSearchURL: "https://search.test/works"}
	got, err := c.Classify(context.Background(), "prompt", "input")
	if err != nil {
		t.Fatal(err)
	}
	if got == "" || round != 2 {
		t.Fatalf("unexpected result=%q rounds=%d", got, round)
	}
}
