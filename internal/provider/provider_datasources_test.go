package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
)

// dataSourceTypeNames returns the Terraform type name of every registered data source.
// Registration is the classic silent failure in this codebase (convention C-8): a data
// source that is implemented but not registered compiles cleanly and simply does not
// exist as far as Terraform is concerned.
func dataSourceTypeNames(t *testing.T) []string {
	t.Helper()
	ctx := context.Background()
	p := &slackProvider{version: "test"}

	var names []string
	for _, factory := range p.DataSources(ctx) {
		ds := factory()
		resp := &datasource.MetadataResponse{}
		ds.Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: "slack"}, resp)
		names = append(names, resp.TypeName)
	}
	return names
}

func TestProvider_RegistersUserDataSource(t *testing.T) {
	names := dataSourceTypeNames(t)

	found := false
	for _, n := range names {
		if n == "slack_user" {
			found = true
		}
	}
	if !found {
		t.Errorf("slack_user is not registered; got %v", names)
	}
}

// AC-8: the existing data source must keep working, unchanged.
func TestProvider_StillRegistersUserIdsDataSource(t *testing.T) {
	names := dataSourceTypeNames(t)

	found := false
	for _, n := range names {
		if n == "slack_user_ids" {
			found = true
		}
	}
	if !found {
		t.Errorf("slack_user_ids must remain registered; got %v", names)
	}
}
