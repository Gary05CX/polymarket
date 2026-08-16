package discovery

import (
	"encoding/json"
	"testing"
)

func TestParseOfficialFromMetadata(t *testing.T) {
	raw := []byte(`{
		"slug":"btc-updown-5m-x",
		"closed":true,
		"eventMetadata":{"finalPrice":62928.66,"priceToBeat":62926.70},
		"markets":[{"outcomes":"[\"Up\",\"Down\"]","outcomePrices":"[\"1\",\"0\"]","closed":true,"automaticallyResolved":true,"umaResolutionStatus":"resolved"}]
	}`)
	var ev gammaEvent
	if err := json.Unmarshal(raw, &ev); err != nil {
		t.Fatal(err)
	}
	res := parseOfficial(ev)
	if !res.Ready || res.Outcome != "Up" {
		t.Fatalf("got ready=%v outcome=%q", res.Ready, res.Outcome)
	}
}

func TestParseOfficialFromPricesDown(t *testing.T) {
	raw := []byte(`{
		"slug":"eth-x",
		"markets":[{"outcomes":"[\"Up\",\"Down\"]","outcomePrices":"[\"0\",\"1\"]","closed":true,"automaticallyResolved":true,"umaResolutionStatus":"resolved"}]
	}`)
	var ev gammaEvent
	if err := json.Unmarshal(raw, &ev); err != nil {
		t.Fatal(err)
	}
	res := parseOfficial(ev)
	if !res.Ready || res.Outcome != "Down" {
		t.Fatalf("got ready=%v outcome=%q", res.Ready, res.Outcome)
	}
}

func TestParseOfficialNotReady(t *testing.T) {
	raw := []byte(`{"slug":"open","markets":[{"outcomes":"[\"Up\",\"Down\"]","outcomePrices":"[\"0.55\",\"0.45\"]","closed":false}]}`)
	var ev gammaEvent
	if err := json.Unmarshal(raw, &ev); err != nil {
		t.Fatal(err)
	}
	res := parseOfficial(ev)
	if res.Ready {
		t.Fatalf("expected not ready, got %q", res.Outcome)
	}
}
