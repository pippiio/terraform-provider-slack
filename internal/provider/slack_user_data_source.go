package provider

// Story: slack_user data source
//
// Input:  a Terraform config setting exactly one of `id` (Slack user ID) or `email`.
// Process:
//   1. Read the config; the framework has already enforced exactly-one-of via
//      ConfigValidators, so no API call happens for an invalid config.
//   2. Call users.info for an id, or users.lookupByEmail for an email.
//   3. On a Slack error, translate the error code into an actionable diagnostic --
//      notably naming the scope to add when the token lacks users:read.email.
//   4. Map the user object onto the schema, preserving null for fields Slack omitted.
// Output: state holding the full user object, with a nested `profile` block.
//
// Dependencies: slackclient.GetUserByID / GetUserByEmail.
// Side effects: one HTTPS GET per read. No writes.

import (
	"context"
	"fmt"

	"terraform-provider-slack/internal/slackclient"

	"github.com/hashicorp/terraform-plugin-framework-validators/datasourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource                     = &userDataSource{}
	_ datasource.DataSourceWithConfigure        = &userDataSource{}
	_ datasource.DataSourceWithConfigValidators = &userDataSource{}
)

func NewUserDataSource() datasource.DataSource {
	return &userDataSource{}
}

type userDataSource struct {
	client *slackclient.Client
}

type userDataSourceModel struct {
	ID    types.String `tfsdk:"id"`
	Email types.String `tfsdk:"email"`

	TeamID   types.String `tfsdk:"team_id"`
	Name     types.String `tfsdk:"name"`
	RealName types.String `tfsdk:"real_name"`
	Deleted  types.Bool   `tfsdk:"deleted"`
	Color    types.String `tfsdk:"color"`
	TZ       types.String `tfsdk:"tz"`
	TZLabel  types.String `tfsdk:"tz_label"`
	TZOffset types.Int64  `tfsdk:"tz_offset"`

	IsAdmin           types.Bool `tfsdk:"is_admin"`
	IsOwner           types.Bool `tfsdk:"is_owner"`
	IsPrimaryOwner    types.Bool `tfsdk:"is_primary_owner"`
	IsRestricted      types.Bool `tfsdk:"is_restricted"`
	IsUltraRestricted types.Bool `tfsdk:"is_ultra_restricted"`
	IsBot             types.Bool `tfsdk:"is_bot"`
	IsAppUser         types.Bool `tfsdk:"is_app_user"`
	IsEmailConfirmed  types.Bool `tfsdk:"is_email_confirmed"`
	Has2FA            types.Bool `tfsdk:"has_2fa"`

	Updated types.Int64  `tfsdk:"updated"`
	Profile types.Object `tfsdk:"profile"`
}

// profileFieldAttrTypes is the shape of one custom profile field. The field IDs keying
// the map are tenant-defined and cannot be enumerated, but this inner shape is stable.
func profileFieldAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"value": types.StringType,
		"alt":   types.StringType,
	}
}

func profileAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"real_name":               types.StringType,
		"real_name_normalized":    types.StringType,
		"display_name":            types.StringType,
		"display_name_normalized": types.StringType,
		"first_name":              types.StringType,
		"last_name":               types.StringType,
		"email":                   types.StringType,
		"title":                   types.StringType,
		"phone":                   types.StringType,
		"skype":                   types.StringType,
		"team":                    types.StringType,
		"status_text":             types.StringType,
		"status_emoji":            types.StringType,
		"status_expiration":       types.Int64Type,
		"avatar_hash":             types.StringType,
		"image_24":                types.StringType,
		"image_32":                types.StringType,
		"image_48":                types.StringType,
		"image_72":                types.StringType,
		"image_192":               types.StringType,
		"image_512":               types.StringType,
		"image_1024":              types.StringType,
		"image_original":          types.StringType,
		"is_custom_image":         types.BoolType,
		"bot_id":                  types.StringType,
		"api_app_id":              types.StringType,
		"fields":                  types.MapType{ElemType: types.ObjectType{AttrTypes: profileFieldAttrTypes()}},
	}
}

func (d *userDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_user"
}

func (d *userDataSource) ConfigValidators(_ context.Context) []datasource.ConfigValidator {
	return []datasource.ConfigValidator{
		datasourcevalidator.ExactlyOneOf(
			path.MatchRoot("id"),
			path.MatchRoot("email"),
		),
	}
}

func (d *userDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Looks up a single Slack user by ID or email address. " +
			"Exactly one of `id` or `email` must be set. " +
			"Requires the `users:read` scope; `users:read.email` is additionally required " +
			"for lookup by email and for the `email` attribute to be populated.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Slack user ID, e.g. `W012A3CDE`. Set this to look the user up by ID. Computed when looking up by email.",
				Optional:    true,
				Computed:    true,
			},
			"email": schema.StringAttribute{
				Description: "Email address. Set this to look the user up by email. Computed from the user's profile otherwise. Null if the token lacks the `users:read.email` scope.",
				Optional:    true,
				Computed:    true,
			},
			"team_id": schema.StringAttribute{
				Description: "ID of the workspace the user belongs to.",
				Computed:    true,
			},
			"name": schema.StringAttribute{
				Description: "The user's Slack handle, not their display name.",
				Computed:    true,
			},
			"real_name": schema.StringAttribute{
				Description: "The user's real name.",
				Computed:    true,
			},
			"deleted": schema.BoolAttribute{
				Description: "True if the account has been deactivated.",
				Computed:    true,
			},
			"color": schema.StringAttribute{
				Description: "Hex colour Slack uses for this user.",
				Computed:    true,
			},
			"tz": schema.StringAttribute{
				Description: "The user's timezone, e.g. `America/Los_Angeles`.",
				Computed:    true,
			},
			"tz_label": schema.StringAttribute{
				Description: "Human-readable timezone label, e.g. `Pacific Daylight Time`.",
				Computed:    true,
			},
			"tz_offset": schema.Int64Attribute{
				Description: "Offset from UTC in seconds.",
				Computed:    true,
			},
			"is_admin": schema.BoolAttribute{
				Description: "True if the user is a workspace admin.",
				Computed:    true,
			},
			"is_owner": schema.BoolAttribute{
				Description: "True if the user is a workspace owner.",
				Computed:    true,
			},
			"is_primary_owner": schema.BoolAttribute{
				Description: "True if the user is the primary owner of the workspace.",
				Computed:    true,
			},
			"is_restricted": schema.BoolAttribute{
				Description: "True if the user is a multi-channel guest.",
				Computed:    true,
			},
			"is_ultra_restricted": schema.BoolAttribute{
				Description: "True if the user is a single-channel guest.",
				Computed:    true,
			},
			"is_bot": schema.BoolAttribute{
				Description: "True if the user is a bot.",
				Computed:    true,
			},
			"is_app_user": schema.BoolAttribute{
				Description: "True if the user is an authorised app user.",
				Computed:    true,
			},
			"is_email_confirmed": schema.BoolAttribute{
				Description: "True if the user has confirmed their email address.",
				Computed:    true,
			},
			"has_2fa": schema.BoolAttribute{
				Description: "True if the user has two-factor authentication enabled. Only returned to admin tokens.",
				Computed:    true,
			},
			"updated": schema.Int64Attribute{
				Description: "Unix timestamp of the last change to the user's profile.",
				Computed:    true,
			},
			"profile": schema.SingleNestedAttribute{
				Description: "The user's profile. Fields Slack does not return are null.",
				Computed:    true,
				Attributes: map[string]schema.Attribute{
					"real_name":               schema.StringAttribute{Description: "Real name as entered by the user.", Computed: true},
					"real_name_normalized":    schema.StringAttribute{Description: "Real name with non-Latin characters normalised.", Computed: true},
					"display_name":            schema.StringAttribute{Description: "Display name shown in Slack.", Computed: true},
					"display_name_normalized": schema.StringAttribute{Description: "Display name with non-Latin characters normalised.", Computed: true},
					"first_name":              schema.StringAttribute{Description: "First name.", Computed: true},
					"last_name":               schema.StringAttribute{Description: "Last name.", Computed: true},
					"email":                   schema.StringAttribute{Description: "Email address. Null unless the token holds the `users:read.email` scope.", Computed: true},
					"title":                   schema.StringAttribute{Description: "Job title.", Computed: true},
					"phone":                   schema.StringAttribute{Description: "Phone number.", Computed: true},
					"skype":                   schema.StringAttribute{Description: "Skype handle.", Computed: true},
					"team":                    schema.StringAttribute{Description: "ID of the workspace this profile belongs to.", Computed: true},
					"status_text":             schema.StringAttribute{Description: "Current custom status text.", Computed: true},
					"status_emoji":            schema.StringAttribute{Description: "Current custom status emoji.", Computed: true},
					"status_expiration":       schema.Int64Attribute{Description: "Unix timestamp when the status expires, or 0 if it does not.", Computed: true},
					"avatar_hash":             schema.StringAttribute{Description: "Hash of the user's avatar.", Computed: true},
					"image_24":                schema.StringAttribute{Description: "URL of the 24px avatar.", Computed: true},
					"image_32":                schema.StringAttribute{Description: "URL of the 32px avatar.", Computed: true},
					"image_48":                schema.StringAttribute{Description: "URL of the 48px avatar.", Computed: true},
					"image_72":                schema.StringAttribute{Description: "URL of the 72px avatar.", Computed: true},
					"image_192":               schema.StringAttribute{Description: "URL of the 192px avatar.", Computed: true},
					"image_512":               schema.StringAttribute{Description: "URL of the 512px avatar.", Computed: true},
					"image_1024":              schema.StringAttribute{Description: "URL of the 1024px avatar.", Computed: true},
					"image_original":          schema.StringAttribute{Description: "URL of the original uploaded avatar. Null unless a custom image is set.", Computed: true},
					"is_custom_image":         schema.BoolAttribute{Description: "True if the user uploaded a custom avatar.", Computed: true},
					"bot_id":                  schema.StringAttribute{Description: "Bot ID, for bot users.", Computed: true},
					"api_app_id":              schema.StringAttribute{Description: "App ID, for app users.", Computed: true},
					"fields": schema.MapNestedAttribute{
						Description: "Custom profile fields defined by the workspace, keyed by Slack's field ID " +
							"(e.g. `Xf0123456`). The keys are workspace-specific, so they cannot be enumerated in " +
							"the schema. Null if Slack did not return the field; an empty map if the user has no " +
							"custom fields set. Field IDs can be mapped to human-readable labels with Slack's " +
							"`team.profile.get` method.",
						Computed: true,
						NestedObject: schema.NestedAttributeObject{
							Attributes: map[string]schema.Attribute{
								"value": schema.StringAttribute{Description: "The field's value for this user.", Computed: true},
								"alt":   schema.StringAttribute{Description: "Alternate representation, used by some field types (often empty).", Computed: true},
							},
						},
					},
				},
			},
		},
	}
}

func (d *userDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*slackclient.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Data Source Configure Type",
			fmt.Sprintf("Expected *slackclient.Client, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	d.client = client
}

func (d *userDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config userDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// ConfigValidators has already guaranteed exactly one of these is set.
	var (
		user       *slackclient.User
		err        error
		lookupByID = !config.ID.IsNull()
		identifier string
	)

	if lookupByID {
		identifier = config.ID.ValueString()
		user, err = d.client.GetUserByID(identifier)
	} else {
		identifier = config.Email.ValueString()
		user, err = d.client.GetUserByEmail(identifier)
	}

	if err != nil {
		summary, detail := lookupErrorDiagnostic(err, lookupByID, identifier)
		resp.Diagnostics.AddError(summary, detail)
		return
	}

	state, diags := userToModel(ctx, user)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
