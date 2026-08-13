package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestDownloadClientModelToAPI(t *testing.T) {
	r := &downloadClientResource{}
	model := &downloadClientModel{
		Name:                     types.StringValue("Transmission"),
		Enable:                   types.BoolValue(true),
		Protocol:                 types.StringValue("torrent"),
		Priority:                 types.Int64Value(1),
		Implementation:           types.StringValue("Transmission"),
		ConfigContract:           types.StringValue("TransmissionSettings"),
		RemoveCompletedDownloads: types.BoolValue(true),
		RemoveFailedDownloads:    types.BoolValue(true),
		Fields: []fieldModel{
			{Name: types.StringValue("host"), Value: types.StringValue("localhost")},
			{Name: types.StringValue("port"), Value: types.StringValue("9091")},
			{Name: types.StringValue("password"), SensitiveValue: types.StringValue("secretpwd")},
		},
	}

	api := r.modelToAPI(model)
	if api.Name != "Transmission" || api.Protocol != "torrent" {
		t.Errorf("expected Transmission/torrent, got %s/%s", api.Name, api.Protocol)
	}
	if len(api.Fields) != 3 {
		t.Fatalf("expected 3 fields, got %d", len(api.Fields))
	}
	if api.Fields[2].Value != "secretpwd" {
		t.Errorf("expected secretpwd for sensitive field, got %v", api.Fields[2].Value)
	}
}

func TestIndexerModelToAPI(t *testing.T) {
	r := &indexerResource{}
	model := &indexerModel{
		Name:           types.StringValue("Prowlarr"),
		Enable:         types.BoolValue(true),
		Protocol:       types.StringValue("torrent"),
		Priority:       types.Int64Value(25),
		Implementation: types.StringValue("Torznab"),
		ConfigContract: types.StringValue("TorznabSettings"),
		AppProfileID:   types.Int64Value(1),
		Fields: []fieldModel{
			{Name: types.StringValue("baseUrl"), Value: types.StringValue("http://prowlarr:9696/1")},
			{Name: types.StringValue("apiKey"), SensitiveValue: types.StringValue("myapikey")},
		},
	}

	api := r.modelToAPI(model)
	if api.Name != "Prowlarr" || api.Implementation != "Torznab" {
		t.Errorf("expected Prowlarr/Torznab, got %s/%s", api.Name, api.Implementation)
	}
	if len(api.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(api.Fields))
	}
	if api.Fields[1].Value != "myapikey" {
		t.Errorf("expected myapikey for sensitive field, got %v", api.Fields[1].Value)
	}
}
