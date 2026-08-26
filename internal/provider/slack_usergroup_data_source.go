package provider

// Story: slack_usergroup data source
//
// Input:  a config setting exactly one of `id` or `handle`.
// Process:
//   1. List user groups, including disabled ones -- their handles stay reserved, so
//      config may legitimately need to see them.
//   2. Match on whichever identifier was supplied.
//   3. Fail if nothing matches, rather than returning an empty group.
// Output: state describing the group, including the flags that say whether its
//         membership is ours to manage.
//
// Dependencies: slackclient.ListUserGroups.
// Side effects: one read per refresh.

import (
	"context"
	"fmt"

	"terraform-provider-slack/internal/slackclient"

	"github.com/hashicorp/terraform-plugin-framework-validators/datasourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource                     = &userGroupDataSource{}
	_ datasource.DataSourceWithConfigure        = &userGroupDataSource{}
	_ datasource.DataSourceWithConfigValidators = &userGroupDataSource{}
)

func NewUserGroupDataSource() datasource.DataSource {
	return &userGroupDataSource{}
}

type userGroupDataSource struct {
	client *slackclient.Client
}

type userGroupDataSourceModel struct {
	ID          types.String `tfsdk:"id"`
	Handle      types.String `tfsdk:"handle"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Channels    types.Set    `tfsdk:"channels"`
	Users       types.Set    `tfsdk:"users"`

	TeamID             types.String `tfsdk:"team_id"`
	UserCount          types.Int64  `tfsdk:"user_count"`
	DateCreate         types.Int64  `tfsdk:"date_create"`
	DateUpdate         types.Int64  `tfsdk:"date_update"`
	IsDisabled         types.Bool   `tfsdk:"is_disabled"`
	IsIDPGroup         types.Bool   `tfsdk:"is_idp_group"`
	IsMembershipLocked types.Bool   `tfsdk:"is_membership_locked"`
	IsExternal         types.Bool   `tfsdk:"is_external"`
}

func (d *userGroupDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_usergroup"
}

func (d *userGroupDataSource) ConfigValidators(_ context.Context) []datasource.ConfigValidator {
	return []datasource.ConfigValidator{
		datasourcevalidator.ExactlyOneOf(
			path.MatchRoot("id"),
			path.MatchRoot("handle"),
		),
	}
}

func (d *userGroupDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Reads a Slack user group by ID or handle. Exactly one of `id` or `handle` must be set.\n\n" +
			"**Requires a paid Slack plan.** User groups are unavailable on the free plan, where every " +
			"`usergroups.*` call fails with Slack's `paid_only` error. Requires the `usergroups:read` scope.\n\n" +
			"Disabled groups are returned as well as active ones — Slack has no delete for user groups, " +
			"and a disabled group keeps its handle reserved. Check `is_disabled` to tell them apart.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Slack user group ID (`S…`). Set this to look the group up by ID.",
				Optional:    true,
				Computed:    true,
			},
			"handle": schema.StringAttribute{
				Description: "The `@mention` abbreviation, without the `@`. Set this to look the group up by handle.",
				Optional:    true,
				Computed:    true,
			},
			"name":        schema.StringAttribute{Description: "Display name of the user group.", Computed: true},
			"description": schema.StringAttribute{Description: "Purpose of the group.", Computed: true},
			"channels": schema.SetAttribute{
				Description: "Default channel IDs new members are added to.",
				ElementType: types.StringType,
				Computed:    true,
			},
			"users": schema.SetAttribute{
				Description: "Slack user IDs currently in the group.",
				ElementType: types.StringType,
				Computed:    true,
			},
			"team_id":     schema.StringAttribute{Description: "ID of the workspace the group belongs to.", Computed: true},
			"user_count":  schema.Int64Attribute{Description: "Number of members Slack reports.", Computed: true},
			"date_create": schema.Int64Attribute{Description: "Unix timestamp when the group was created.", Computed: true},
			"date_update": schema.Int64Attribute{Description: "Unix timestamp of the last change.", Computed: true},
			"is_disabled": schema.BoolAttribute{
				Description: "True if the group is disabled. Slack has no delete, so destroyed groups remain in this state.",
				Computed:    true,
			},
			"is_idp_group": schema.BoolAttribute{
				Description: "True if the group is synced from an identity provider, which owns its membership.",
				Computed:    true,
			},
			"is_membership_locked": schema.BoolAttribute{
				Description: "True if Slack has locked the group's membership.",
				Computed:    true,
			},
			"is_external": schema.BoolAttribute{Description: "True if the group originates outside this workspace.", Computed: true},
		},
	}
}

func (d *userGroupDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	clients := providerClientsFrom(req.ProviderData, "Data Source", &resp.Diagnostics)
	if clients == nil {
		return
	}

	// Reading user groups works with the bot token; only managing them needs the user token.
	d.client = clients.Bot
}

func (d *userGroupDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config userGroupDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	byID := !config.ID.IsNull()
	identifier := config.Handle.ValueString()
	if byID {
		identifier = config.ID.ValueString()
	}

	groups, err := d.client.ListUserGroups(true, true)
	if err != nil {
		summary, detail := userGroupErrorDiagnostic(err, "read", identifier)
		resp.Diagnostics.AddError(summary, detail)
		return
	}

	var found *slackclient.UserGroup
	for i := range groups {
		if (byID && groups[i].ID == identifier) || (!byID && groups[i].Handle == identifier) {
			found = &groups[i]
			break
		}
	}

	if found == nil {
		kind := "handle"
		if byID {
			kind = "ID"
		}
		resp.Diagnostics.AddError(
			"Slack user group not found",
			fmt.Sprintf(
				"No Slack user group was found with the %s %q.\n\n"+
					"Disabled groups are included in the search, so this means no group exists with that "+
					"%s at all. Check for a typo, or that the token can see the group.",
				kind, identifier, kind,
			),
		)
		return
	}

	state, diags := userGroupToDataSourceModel(ctx, found)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
