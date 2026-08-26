package provider

import (
	"context"
	"terraform-provider-slack/internal/slackclient"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = &userIdDataSource{}
	_ datasource.DataSourceWithConfigure = &userIdDataSource{}
)

func NewUserIdDataSource() datasource.DataSource {
	return &userIdDataSource{}
}

type userIdDataSource struct {
	client *slackclient.Client
}

type userIdDataSourceModel struct {
	Usernames   types.Set    `tfsdk:"usernames"`
	Slack_IDs   types.Map    `tfsdk:"slack_ids"`
	LastUpdated types.String `tfsdk:"last_updated"`
}

func (d *userIdDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_user_ids"
}

func (d *userIdDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"usernames": schema.SetAttribute{
				Description: "Set of usernames to get userids for.",
				ElementType: types.StringType,
				Required:    true,
			},
			"slack_ids": schema.MapAttribute{
				Description: "The map of usernames and userids",
				Computed:    true,
				ElementType: types.StringType,
			},
			"last_updated": schema.StringAttribute{
				Computed: true,
			},
		},
	}
}

func (d *userIdDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state userIdDataSourceModel

	diags := req.Config.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	var usernames []string
	diags = state.Usernames.ElementsAs(ctx, &usernames, false)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	apiResp, err := d.client.ReadUserIds()
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to read Slack users",
			err.Error(),
		)
		return
	}

	requested := make(map[string]struct{})
	for _, u := range usernames {
		requested[u] = struct{}{}
	}

	result := make(map[string]string)

	for _, member := range apiResp.Members {
		if _, ok := requested[member.Name]; ok {
			result[member.Name] = member.Id
		}
	}

	slackMap, diags := types.MapValueFrom(ctx, types.StringType, result)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	state.Slack_IDs = slackMap
	state.LastUpdated = types.StringValue(time.Now().Format(time.RFC850))

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (d *userIdDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	clients := providerClientsFrom(req.ProviderData, "Data Source", &resp.Diagnostics)
	if clients == nil {
		return
	}

	d.client = clients.Bot
}
