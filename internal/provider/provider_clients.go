package provider

// Story: provider client container
//
// Input:  the resolved bot token and optional user token from provider configuration.
// Process: build one Slack client per token and hand both to every resource and data
//          source, which each pick the one they need.
// Output: a *providerClients passed via ResourceData / DataSourceData.
//
// Why two clients: Slack refuses usergroups.create for bot tokens in workspaces that
// restrict user group management, answering permission_denied rather than missing_scope.
// Managing user groups therefore needs a user token (xoxp-), while everything else in
// this provider works with the bot token. Rather than overload one client, the provider
// carries both and each type states which it requires.

import (
	"fmt"
	"os"

	"terraform-provider-slack/internal/slackclient"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// providerClients holds the Slack clients available to resources and data sources.
type providerClients struct {
	// Bot is always present, built from the required bot token.
	Bot *slackclient.Client
	// User is nil unless a user token was configured. Only user-group management needs it.
	User *slackclient.Client
}

// resolveToken applies the provider's env-first, config-overrides precedence
// (architecture.md I-5): read the environment variable, then let an explicit
// configuration value win.
func resolveToken(configured types.String, envVar string) string {
	value := os.Getenv(envVar)
	if !configured.IsNull() {
		value = configured.ValueString()
	}
	return value
}

// providerClientsFrom extracts the client container from ProviderData.
//
// kind reads as "Resource" or "Data Source" so the diagnostic names the right thing.
func providerClientsFrom(data any, kind string, diags *diag.Diagnostics) *providerClients {
	clients, ok := data.(*providerClients)
	if !ok {
		diags.AddError(
			fmt.Sprintf("Unexpected %s Configure Type", kind),
			fmt.Sprintf(
				"Expected *providerClients holding a *slackclient.Client, got: %T. "+
					"Please report this issue to the provider developers.", data,
			),
		)
		return nil
	}
	return clients
}

// requireUserClient returns the user client, or emits a diagnostic explaining how to
// configure one.
//
// This is what enforces "user groups can only be managed when a user token is set".
func requireUserClient(clients *providerClients, diags *diag.Diagnostics) *slackclient.Client {
	if clients.User != nil {
		return clients.User
	}

	diags.AddError(
		"Managing Slack user groups requires a user token",
		"The `slack_usergroup` resource needs a Slack **user** token (`xoxp-…`), which is not "+
			"configured.\n\n"+
			"Slack refuses `usergroups.create` for bot tokens in workspaces that restrict who may "+
			"manage user groups, answering `permission_denied` rather than a missing-scope error. "+
			"Reading groups works with the bot token, so the `slack_usergroup` *data source* is "+
			"unaffected — only creating and changing them needs the user token.\n\n"+
			"Set it on the provider:\n\n"+
			"    provider \"slack\" {\n"+
			"      token      = var.slack_bot_token   # xoxb-…\n"+
			"      user_token = var.slack_user_token  # xoxp-…\n"+
			"    }\n\n"+
			"or via the SLACK_USER_TOKEN environment variable.\n\n"+
			"The user token must carry the `usergroups:write` scope, and must belong to someone "+
			"permitted to manage user groups in the workspace.",
	)
	return nil
}
