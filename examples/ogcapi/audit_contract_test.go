package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAuditAdvertisedRESTBindingsAreMounted(t *testing.T) {
	host := exampleServer(t)
	status, data, _ := fetch(t, host, "GET", "/wmts?SERVICE=WMTS&REQUEST=GetCapabilities", "")
	if status != 200 || !strings.Contains(string(data), "ResourceURL") {
		t.Fatal("missing fixture metadata")
	}
	for _, path := range []string{"/tiles/world/default/local/L00/0/0.png", "/wmts/1.0.0/WMTSCapabilities.xml"} {
		status, data, _ := fetch(t, host, "GET", path, "")
		if status != 200 {
			t.Errorf("advertised binding %s returned %d: %s", path, status, data)
		}
	}
}
func TestAuditOpenAPIAllowsActualRawJSONResult(t *testing.T) {
	host := exampleServer(t)
	_, data, _ := fetch(t, host, "GET", "/ogc/api", "")
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	schema := document["paths"].(map[string]any)["/processes/{processId}/execution"].(map[string]any)["post"].(map[string]any)["responses"].(map[string]any)["200"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
	status, actual, _ := fetch(t, host, "POST", "/ogc/processes/echo/execution", `{"inputs":{"message":"ready"}}`)
	var result any
	if err := json.Unmarshal(actual, &result); err != nil {
		t.Fatal(err)
	}
	if _, object := result.(map[string]any); status == 200 && schema["type"] == "object" && !object {
		t.Errorf("OpenAPI declares object but actual successful raw result=%s", actual)
	}
}
