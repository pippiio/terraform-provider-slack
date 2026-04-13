package provider

import (
	"context"
	"os"

	"terraform-provider-slack/internal/slackclient"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ provider.Provider = &slackProvider{}
)

// New is a helper function to simplify provider server and testing implementation.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &slackProvider{
			version: version,
		}
	}
}

// hashicupsProvider is the provider implementation.
type slackProvider struct {
	// version is set to the provider version on release, "dev" when the
	// provider is built and ran locally, and "test" when running acceptance
	// testing.
	version string
}

type slackProviderModel struct {
	Host  types.String `tfsdk:"host"`
	Token types.String `tfsdk:"token"`
}

// Metadata returns the provider type name.
func (p *slackProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "slack"
	resp.Version = p.version
}

// Schema defines the provider-level schema for configuration data.
func (p *slackProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"host": schema.StringAttribute{
				Description: "URI for Slack API. May also be provided via SLACK_HOST environment variable.",
				Optional:    true,
			},
			"token": schema.StringAttribute{
				Description: "Bearer Token for Slack API. May also be provided via SLACK_TOKEN environment variable.",
				Optional:    true,
				Sensitive:   true,
			},
		},
	}
}

// Configure prepares a Slack API client for data sources and resources.
func (p *slackProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config slackProviderModel
	diags := req.Config.Get(ctx, &config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if config.Host.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("host"),
			"Unknown Slack API Host",
			"The provider cannot create the Slack API client as there is an unknown configuration value for the Slack API host. "+
				"Either target apply the source of the value first, set the value statically in the configuration, or use the SLACK_HOST environment variable.",
		)
	}

	if config.Token.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("token"),
			"Unknown Slack API token",
			"The provider cannot create the Slack API client as there is an unknown configuration value for the Slack API token. "+
				"Either target apply the source of the value first, set the value statically in the configuration, or use the SLACK_TOKEN environment variable.",
		)
	}

	if resp.Diagnostics.HasError() {
		return
	}

	host := os.Getenv("SLACK_HOST")
	token := os.Getenv("SLACK_TOKEN")

	if !config.Host.IsNull() {
		host = config.Host.ValueString()
	}

	if !config.Token.IsNull() {
		token = config.Token.ValueString()
	}

	if host == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("host"),
			"Missing Slack API Host",
			"The provider cannot create the Slack API client as there is a missing or empty value for the Slack API host. "+
				"Set the host value in the configuration or use the SLACK_HOST environment variable. "+
				"If either is already set, ensure the value is not empty.",
		)
	}
	if token == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("token"),
			"Missing Slack API Token",
			"The provider cannot create the Slack API client as there is a missing or empty value for the Slack API token. "+
				"Set the token value in the configuration or use the SLACK_TOKEN environment variable. "+
				"If either is already set, ensure the value is not empty.",
		)
	}

	if resp.Diagnostics.HasError() {
		return
	}

	client, err := slackclient.NewClient(&host, &token)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to Create Slack API Client",
			"An unexpected error occurred when creating the Slack API client. "+
				"If the error is not clear, please contact the provider developers.\n\n"+
				"Slack Client Error: "+err.Error(),
		)
		return
	}

	resp.DataSourceData = client
	resp.ResourceData = client
}

// DataSources defines the data sources implemented in the provider.
func (p *slackProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewUserIdDataSource,
	}
}

// Resources defines the resources implemented in the provider.
func (p *slackProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewMessageResource,
	}
}
