package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ datasource.DataSource = &HelloWorldDataSource{}
)

// NewSlackDataSource is a helper function to simplify the provider implementation.
func NewHelloWorldDataSource() datasource.DataSource {
	return &HelloWorldDataSource{}
}

// SlackDataSource is the data source implementation.
type HelloWorldDataSource struct{}

type state struct {
	Hello types.String `tfsdk:"hello"`
}

// Metadata returns the data source type name.
func (d *HelloWorldDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_hello_world"
}

// Schema defines the schema for the data source.
func (d *HelloWorldDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"hello": schema.StringAttribute{
				Computed: true,
			},
		},
	}
}

// Read refreshes the Terraform state with the latest data.
func (d *HelloWorldDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state state
	state.Hello = types.StringValue("World")

	resp.State.Set(ctx, &state)
}
