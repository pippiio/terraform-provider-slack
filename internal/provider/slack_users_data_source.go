package provider

// Story: slack_users data source
//
// Input:  a config with at most one selector (emails, usernames, channel) and any
//         number of tri-state filters.
// Process:
//   1. Read the config; ConfigValidators has already enforced at-most-one-of.
//   2. Resolve the container, if there is one, into a set of member IDs.
//   3. Make one paginated users.list pass, keeping users the selector matches.
//   4. Apply the tri-state filters to what matched.
//   5. Apply the no-match policy: unresolved inputs, then emptiness.
// Output: a map of full user objects keyed by user ID, an ID set, and a count.
//
// Dependencies: slackclient.ListChannelMembers and the users.list scan; userToObject
// from slack_user_mapping.go for the element shape.
// Side effects: one users.list page-walk per read, plus one conversations.members
// page-walk when the channel selector is set. No writes.
//
// Why one scan rather than per-input lookups: users.lookupByEmail does not match
// deactivated accounts and users.list does, so a size-triggered hybrid would return
// different users for the same config depending on how many emails were passed, and
// the `deleted` filter would be unimplementable on half of it.

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
	_ datasource.DataSource                     = &usersDataSource{}
	_ datasource.DataSourceWithConfigure        = &usersDataSource{}
	_ datasource.DataSourceWithConfigValidators = &usersDataSource{}
)

func NewUsersDataSource() datasource.DataSource {
	return &usersDataSource{}
}

type usersDataSource struct {
	client *slackclient.Client
}

type usersDataSourceModel struct {
	// Selectors. At most one may be set; none means the whole workspace.
	Emails    types.Set    `tfsdk:"emails"`
	Usernames types.Set    `tfsdk:"usernames"`
	Channel   types.String `tfsdk:"channel"`

	// Filters. Null means "don't care" -- the three-way distinction is the point.
	IsBot             types.Bool `tfsdk:"is_bot"`
	Deleted           types.Bool `tfsdk:"deleted"`
	IsRestricted      types.Bool `tfsdk:"is_restricted"`
	IsUltraRestricted types.Bool `tfsdk:"is_ultra_restricted"`
	IsAdmin           types.Bool `tfsdk:"is_admin"`
	IsAppUser         types.Bool `tfsdk:"is_app_user"`

	ErrorOnNoMatch types.Bool `tfsdk:"error_on_no_match"`

	Users     types.Map   `tfsdk:"users"`
	UserIDs   types.Set   `tfsdk:"user_ids"`
	UserCount types.Int64 `tfsdk:"user_count"`
}

func (d *usersDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_users"
}

// ConfigValidators enforces at-most-one selector.
//
// Conflicting is the validator for this: it errors only when two or more are set, and
// permits zero, which is the whole-workspace case. ExactlyOneOf would forbid that.
func (d *usersDataSource) ConfigValidators(_ context.Context) []datasource.ConfigValidator {
	return []datasource.ConfigValidator{
		datasourcevalidator.Conflicting(
			path.MatchRoot("emails"),
			path.MatchRoot("usernames"),
			path.MatchRoot("channel"),
		),
	}
}

func (d *usersDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Looks up a set of Slack users. At most one of `emails`, `usernames` or " +
			"`channel` may be set; setting none returns every user in the workspace. The " +
			"optional boolean filters narrow whatever the selector matched.\n\n" +
			"Every read costs one paginated pass over `users.list`, regardless of how many " +
			"users come back, because Slack's membership endpoints return IDs rather than " +
			"user objects. Requires `users:read`; `users:read.email` is additionally required " +
			"for the `emails` selector and for `email` to be populated.",
		Attributes: map[string]schema.Attribute{
			"emails": schema.SetAttribute{
				Description: "Email addresses to resolve. Matched case-insensitively against " +
					"`profile.email`. Requires the `users:read.email` scope; without it Slack " +
					"omits the field entirely and nothing can match.",
				ElementType: types.StringType,
				Optional:    true,
			},
			"usernames": schema.SetAttribute{
				Description: "Slack handles to resolve, matched against `name`. Exact and " +
					"case-sensitive, unlike `emails`.",
				ElementType: types.StringType,
				Optional:    true,
			},
			"channel": schema.StringAttribute{
				Description: "Channel ID, e.g. `C012AB3CD`, whose members to return. This is an " +
					"ID, not a name — Slack's endpoint takes no name. Requires `channels:read` " +
					"for public channels or `groups:read` for private ones.",
				Optional: true,
			},

			"is_bot":              boolFilter("the user is a bot"),
			"deleted":             boolFilter("the account has been deactivated"),
			"is_restricted":       boolFilter("the user is a multi-channel guest"),
			"is_ultra_restricted": boolFilter("the user is a single-channel guest"),
			"is_admin":            boolFilter("the user is a workspace admin"),
			"is_app_user":         boolFilter("the user is an authorised app user"),

			"error_on_no_match": schema.BoolAttribute{
				Description: "Whether a read with nothing to show fails. Defaults to `true`, " +
					"which covers two cases: an email or username that matched no account, " +
					"named individually; and an empty result after filtering.\n\n" +
					"Note the default makes a channel filtered down to zero matches an error. " +
					"Set `false` where an empty result is a legitimate state. A channel or " +
					"user group that does not exist is always an error, either way — that is a " +
					"broken reference, not an empty set.",
				Optional: true,
			},

			// Declared as a MapAttribute over the shared object type rather than a
			// MapNestedAttribute. A nested attribute would need every field restated
			// with its own description -- a second copy of the 27-field profile, which
			// is the drift FR-11 exists to prevent. The cost is that the generated docs
			// show the object's shape inline instead of a field table; the schema
			// description points readers at slack_user, which documents each field.
			"users": schema.MapAttribute{
				Description: "The matched users, keyed by Slack user ID. Each value is the same " +
					"object the `slack_user` data source exposes, and is documented there. " +
					"Re-key by any attribute with a `for` expression, e.g. " +
					"`{ for id, u in data.slack_users.this.users : u.name => u }`.",
				Computed:    true,
				ElementType: types.ObjectType{AttrTypes: userAttrTypes()},
			},
			"user_ids": schema.SetAttribute{
				Description: "The matched user IDs. A convenience projection of `users`, for " +
					"wiring straight into `slack_message.slack_ids` or `slack_usergroup.users`.",
				ElementType: types.StringType,
				Computed:    true,
			},
			"user_count": schema.Int64Attribute{
				Description: "How many users matched. Named `user_count` rather than `count` " +
					"because Terraform reserves `count` as a meta-argument.",
				Computed: true,
			},
		},
	}
}

// boolFilter builds one tri-state filter attribute. Leaving it unset means "don't
// care"; setting it requires the user's flag to equal it. There is deliberately no
// default: a data source that silently drops users nobody asked it to drop is the
// mistake slack_user_ids was just fixed for.
func boolFilter(what string) schema.BoolAttribute {
	return schema.BoolAttribute{
		Description: fmt.Sprintf(
			"Keep only users where %s matches this value. Unset means the filter is not applied.",
			what,
		),
		Optional: true,
	}
}

func (d *usersDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *usersDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	// Implemented in Task 3.5. Failing loudly rather than writing empty state keeps a
	// half-built data source from looking like one that matched nothing.
	resp.Diagnostics.AddError(
		"slack_users Read is not implemented yet",
		"This data source is still being built; see track slack-users-data-source, Task 3.5.",
	)
}
