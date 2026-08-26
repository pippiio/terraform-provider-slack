package provider

// Story: slack_usergroup resource
//
// Input:  a Terraform config describing a Slack user group.
// Process:
//   Create  - look for an existing group with the same handle first. Adopt it if it is
//             disabled, refuse if it is active, otherwise create fresh. Then apply members.
//   Read    - find the group by ID via usergroups.list, mapping every field back.
//   Update  - usergroups.update for metadata, usergroups.users.update for members.
//   Delete  - usergroups.disable. Slack has no delete.
// Output: state describing the group.
//
// Dependencies: slackclient usergroups methods.
// Side effects: creates, modifies, and disables real Slack user groups.
//
// Three Slack constraints drive this design more than the requirements do:
//   1. There is no delete. Destroy disables, and the handle stays reserved -- so Create
//      must be able to adopt a disabled group or apply/destroy/apply is a one-way door.
//   2. Membership is replace-only, so `users` is authoritative. Omitting it is the
//      supported way to let Slack own membership.
//   3. Groups synced from an identity provider, or with membership locked, are not ours
//      to manage; writing members to them is refused rather than attempted.

import (
	"context"
	"fmt"

	"terraform-provider-slack/internal/slackclient"

	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &userGroupResource{}
	_ resource.ResourceWithConfigure   = &userGroupResource{}
	_ resource.ResourceWithImportState = &userGroupResource{}
)

func NewUserGroupResource() resource.Resource {
	return &userGroupResource{}
}

type userGroupResource struct {
	client *slackclient.Client
}

type userGroupResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Handle      types.String `tfsdk:"handle"`
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
}

func (r *userGroupResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_usergroup"
}

func (r *userGroupResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Slack user group (an `@mention` group, called a \"subteam\" by the API).\n\n" +
			"**Requires a paid Slack plan.** User groups are not available on the free plan; " +
			"every `usergroups.*` call on a free workspace fails with Slack's `paid_only` error, " +
			"so this resource cannot be used there at all.\n\n" +
			"Requires the `usergroups:read` and `usergroups:write` scopes. Note that Slack also " +
			"gates group creation on a workspace setting: a correctly-scoped token can still be " +
			"refused with `permission_denied` if the workspace restricts who may create user groups.\n\n" +
			"**`terraform destroy` disables this group rather than deleting it.** Slack provides no " +
			"delete for user groups, and a disabled group keeps its name and handle **reserved**. " +
			"Creating a group with the same handle afterwards re-enables and adopts the disabled one.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Slack user group ID (`S…`). Use this with `terraform import`.",
				Computed:    true,
			},
			"name": schema.StringAttribute{
				Description: "Display name of the user group.",
				Required:    true,
			},
			"handle": schema.StringAttribute{
				Description: "The `@mention` abbreviation, without the `@`. Unique per workspace, and " +
					"stays reserved after the group is disabled.",
				Required: true,
			},
			"description": schema.StringAttribute{
				Description: "Purpose of the group.",
				Optional:    true,
			},
			"channels": schema.SetAttribute{
				Description: "Channel IDs new members are added to by default (Slack's `prefs.channels`). " +
					"These are default channels for the group, not a list of channels the group belongs to.",
				ElementType: types.StringType,
				Optional:    true,
			},
			"users": schema.SetAttribute{
				// Slack has no way to express "no members" through usergroups.users.update:
				// an empty list arrives as a single empty-string element and is rejected with
				// invalid_arguments and a regex complaint. Catching it at plan time turns that
				// into an instruction. This is a provider-side constraint, not a Slack rule
				// about minimum group size -- Slack was never observed to forbid empty groups.
				Validators: []validator.Set{
					setvalidator.SizeAtLeast(1),
				},
				Description: "Slack user IDs belonging to the group. **This attribute is authoritative**: " +
					"Slack offers only a replace operation for membership, so anyone added to the group " +
					"by hand in Slack is removed on the next apply, and Slack sends no notification when " +
					"that happens. **Omit this attribute entirely** to let Slack own membership — the " +
					"provider then never touches it. Cannot be used on groups synced from an identity " +
					"provider or with membership locked.\n\n" +
					"Must contain at least one user ID when set — Slack provides no way to express an " +
					"empty group through its membership API. Omit the attribute entirely instead.",
				ElementType: types.StringType,
				Optional:    true,
			},
			"team_id": schema.StringAttribute{
				Description: "ID of the workspace the group belongs to.",
				Computed:    true,
			},
			"user_count": schema.Int64Attribute{
				Description: "Number of members Slack reports for the group.",
				Computed:    true,
			},
			"date_create": schema.Int64Attribute{
				Description: "Unix timestamp when the group was created.",
				Computed:    true,
			},
			"date_update": schema.Int64Attribute{
				Description: "Unix timestamp of the last change to the group.",
				Computed:    true,
			},
			"is_disabled": schema.BoolAttribute{
				Description: "True if the group is disabled. Slack has no delete, so a destroyed group " +
					"remains in the workspace in this state.",
				Computed: true,
			},
			"is_idp_group": schema.BoolAttribute{
				Description: "True if the group is synced from an identity provider. Membership of such " +
					"groups is owned by the IdP and cannot be managed here.",
				Computed: true,
			},
			"is_membership_locked": schema.BoolAttribute{
				Description: "True if Slack has locked the group's membership. `users` cannot be applied " +
					"to such a group.",
				Computed: true,
			},
		},
	}
}

func (r *userGroupResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	clients := providerClientsFrom(req.ProviderData, "Resource", &resp.Diagnostics)
	if clients == nil {
		return
	}

	// Managing user groups requires a user token: Slack refuses usergroups.create for bot
	// tokens in workspaces that restrict user group management.
	userClient := requireUserClient(clients, &resp.Diagnostics)
	if userClient == nil {
		return
	}
	r.client = userClient
}

func (r *userGroupResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// applyMembership writes the member list, refusing groups whose membership is not ours
// to manage. A nil users slice means the configuration omitted `users`, in which case no
// call is made at all (FR-8a).
func (r *userGroupResource) applyMembership(g *slackclient.UserGroup, users []string, diags *diag.Diagnostics) *slackclient.UserGroup {
	if users == nil {
		return g
	}

	if !g.MembershipIsManageable() {
		reason := "its membership is locked by Slack"
		if g.IsIDPGroup != nil && *g.IsIDPGroup {
			reason = "it is synced from an identity provider, which owns its membership"
		}
		diags.AddError(
			"Slack user group membership cannot be managed",
			fmt.Sprintf(
				"The `users` attribute was set on user group %q (%s), but %s.\n\n"+
					"Remove the `users` attribute from this resource and let the upstream system own "+
					"membership. The `is_idp_group` and `is_membership_locked` attributes report this "+
					"state so configuration can branch on it.",
				g.Handle, g.ID, reason,
			),
		)
		return g
	}

	updated, err := r.client.UpdateUserGroupUsers(g.ID, users)
	if err != nil {
		summary, detail := userGroupErrorDiagnostic(err, "set members on", g.Handle)
		diags.AddError(summary, detail)
		return g
	}
	return updated
}

func (r *userGroupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan userGroupResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	channels := setToStrings(ctx, plan.Channels, &resp.Diagnostics)
	users := setToStrings(ctx, plan.Users, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	handle := plan.Handle.ValueString()

	// Slack reserves handles even for disabled groups, so a plain create would fail for
	// anything this configuration previously destroyed. Look first.
	existing, err := r.client.FindUserGroupByHandle(handle)
	if err != nil {
		summary, detail := userGroupErrorDiagnostic(err, "look up", handle)
		resp.Diagnostics.AddError(summary, detail)
		return
	}

	var group *slackclient.UserGroup

	switch {
	case existing == nil:
		group, err = r.client.CreateUserGroup(slackclient.CreateUserGroupRequest{
			Name:        plan.Name.ValueString(),
			Handle:      handle,
			Description: plan.Description.ValueString(),
			Channels:    channels,
		})

	case existing.IsDisabled():
		// Adopt: re-enable and bring it in line with the configuration. Warn, because this
		// revives a group somebody deliberately disabled and overwrites its metadata.
		resp.Diagnostics.AddWarning(
			"Adopted an existing disabled Slack user group",
			fmt.Sprintf(
				"A disabled user group with the handle %q already existed (%s), so it was re-enabled "+
					"and updated to match this configuration rather than creating a new one.\n\n"+
					"Slack never deletes user groups and keeps their handles reserved, so this is the "+
					"only way to re-create a group that was previously destroyed. Its previous name, "+
					"description and channels have been overwritten.",
				handle, existing.ID,
			),
		)
		if _, err = r.client.EnableUserGroup(existing.ID); err == nil {
			name, desc := plan.Name.ValueString(), plan.Description.ValueString()
			group, err = r.client.UpdateUserGroup(slackclient.UpdateUserGroupRequest{
				ID:          existing.ID,
				Name:        &name,
				Description: &desc,
				Channels:    channels,
			})
			// usergroups.update returns a thinner object than usergroups.list and may omit
			// is_idp_group / is_membership_locked. Those flags decide whether membership is
			// ours to write at all, so carry forward what the list told us rather than
			// letting an absent field read as "manageable".
			if group != nil {
				carryManageabilityFlags(existing, group)
			}
		}

	default:
		// An active group already owns this handle. Adopting it would silently transfer
		// ownership of something this configuration never created.
		resp.Diagnostics.AddError(
			"Slack user group handle is already in use",
			fmt.Sprintf(
				"An active Slack user group already uses the handle %q (%s).\n\n"+
					"This resource will not take over a group it did not create. Either choose a "+
					"different handle, or adopt the existing group deliberately:\n\n"+
					"    terraform import slack_usergroup.<name> %s",
				handle, existing.ID, existing.ID,
			),
		)
		return
	}

	if err != nil {
		summary, detail := userGroupErrorDiagnostic(err, "create", handle)
		resp.Diagnostics.AddError(summary, detail)
		return
	}

	group = r.applyMembership(group, users, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	state, diags := userGroupToModel(ctx, group, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *userGroupResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state userGroupResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()

	groups, err := r.client.ListUserGroups(true, true)
	if err != nil {
		summary, detail := userGroupErrorDiagnostic(err, "read", id)
		resp.Diagnostics.AddError(summary, detail)
		return
	}

	var found *slackclient.UserGroup
	for i := range groups {
		if groups[i].ID == id {
			found = &groups[i]
			break
		}
	}

	// Gone from the workspace entirely, or disabled outside Terraform: either way the
	// group this configuration manages no longer exists in a usable form.
	if found == nil || found.IsDisabled() {
		resp.State.RemoveResource(ctx)
		return
	}

	newState, diags := userGroupToModel(ctx, found, state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &newState)...)
}

func (r *userGroupResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state userGroupResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	channels := setToStrings(ctx, plan.Channels, &resp.Diagnostics)
	users := setToStrings(ctx, plan.Users, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	name := plan.Name.ValueString()
	handle := plan.Handle.ValueString()
	desc := plan.Description.ValueString()

	group, err := r.client.UpdateUserGroup(slackclient.UpdateUserGroupRequest{
		ID:          id,
		Name:        &name,
		Handle:      &handle,
		Description: &desc,
		Channels:    channels,
	})
	if err != nil {
		summary, detail := userGroupErrorDiagnostic(err, "update", handle)
		resp.Diagnostics.AddError(summary, detail)
		return
	}

	group = r.applyMembership(group, users, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	newState, diags := userGroupToModel(ctx, group, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	newState.ID = types.StringValue(id)
	resp.Diagnostics.Append(resp.State.Set(ctx, &newState)...)
}

func (r *userGroupResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state userGroupResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Slack has no delete. Disabling is the closest equivalent, and the group's name and
	// handle stay reserved afterwards.
	if _, err := r.client.DisableUserGroup(state.ID.ValueString()); err != nil {
		summary, detail := userGroupErrorDiagnostic(err, "disable", state.Handle.ValueString())
		resp.Diagnostics.AddError(summary, detail)
		return
	}
}
