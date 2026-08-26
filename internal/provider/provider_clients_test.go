package provider

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Slack refuses usergroups.create for bot tokens in this workspace, so managing user
// groups needs a user token (xoxp-) alongside the bot token. The provider therefore
// carries two clients, and the usergroup resource requires the second.

func TestProviderSchema_HasUserTokenAttribute(t *testing.T) {
	p := &slackProvider{version: "test"}
	resp := &provider.SchemaResponse{}
	p.Schema(context.Background(), provider.SchemaRequest{}, resp)

	attr, ok := resp.Schema.Attributes["user_token"]
	if !ok {
		t.Fatal("provider schema has no `user_token` attribute")
	}
	if !attr.IsOptional() {
		t.Error("user_token must be Optional -- it is only needed for user group management")
	}
	if !attr.IsSensitive() {
		t.Error("user_token must be Sensitive")
	}
	d := strings.ToLower(attr.GetDescription())
	if !strings.Contains(d, "xoxp") {
		t.Errorf("description should name the xoxp- token form; got: %s", attr.GetDescription())
	}
	if !strings.Contains(d, "slack_user_token") {
		t.Errorf("description should name the SLACK_USER_TOKEN env var; got: %s", attr.GetDescription())
	}
}

// The bot token stays required; the user token does not.
func TestProviderConfigure_UserTokenIsOptional(t *testing.T) {
	clients := &providerClients{Bot: newStubClient(t, map[string]stub{})}

	if clients.User != nil {
		t.Error("User should be nil when no user token is configured")
	}
	if clients.Bot == nil {
		t.Error("Bot client must always be present")
	}
}

// Managing groups without a user token must fail early and explain what to add.
func TestUserGroupResource_ConfigureRequiresUserToken(t *testing.T) {
	r := &userGroupResource{}
	resp := &resource.ConfigureResponse{}
	r.Configure(context.Background(), resource.ConfigureRequest{
		ProviderData: &providerClients{Bot: newStubClient(t, map[string]stub{})},
	}, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("managing user groups without a user token must fail")
	}
	detail := resp.Diagnostics.Errors()[0].Detail()
	lower := strings.ToLower(detail)
	if !strings.Contains(lower, "user_token") {
		t.Errorf("diagnostic must name the user_token attribute; got: %s", detail)
	}
	if !strings.Contains(lower, "xoxp") {
		t.Errorf("diagnostic should name the token form to create; got: %s", detail)
	}
}

func TestUserGroupResource_ConfigureAcceptsUserToken(t *testing.T) {
	r := &userGroupResource{}
	bot := newStubClient(t, map[string]stub{})
	user := newStubClient(t, map[string]stub{})

	resp := &resource.ConfigureResponse{}
	r.Configure(context.Background(), resource.ConfigureRequest{
		ProviderData: &providerClients{Bot: bot, User: user},
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("a configured user token must be accepted: %v", resp.Diagnostics)
	}
	if r.client != user {
		t.Error("the usergroup resource must use the USER client, not the bot client")
	}
}

// Reading groups works with the bot token, so the data source must not demand a user one.
func TestUserGroupDataSource_UsesBotClient(t *testing.T) {
	d := &userGroupDataSource{}
	bot := newStubClient(t, map[string]stub{})
	user := newStubClient(t, map[string]stub{})

	resp := &datasource.ConfigureResponse{}
	d.Configure(context.Background(), datasource.ConfigureRequest{
		ProviderData: &providerClients{Bot: bot, User: user},
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("data source must configure with a bot token: %v", resp.Diagnostics)
	}
	if d.client != bot {
		t.Error("the usergroup data source should read with the BOT client, not the user client")
	}
}

// A bot-token-only setup must still allow reading groups.
func TestUserGroupDataSource_ConfigureWithoutUserToken(t *testing.T) {
	d := &userGroupDataSource{}
	bot := newStubClient(t, map[string]stub{})

	resp := &datasource.ConfigureResponse{}
	d.Configure(context.Background(), datasource.ConfigureRequest{
		ProviderData: &providerClients{Bot: bot},
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("reading groups must not require a user token: %v", resp.Diagnostics)
	}
	if d.client != bot {
		t.Error("data source should hold the bot client")
	}
}

// The env-var fallback mirrors the bot token's (architecture.md I-5).
func TestProviderConfigure_ReadsUserTokenFromEnv(t *testing.T) {
	t.Setenv("SLACK_USER_TOKEN", "xoxp-from-env")

	if got := os.Getenv("SLACK_USER_TOKEN"); got != "xoxp-from-env" {
		t.Fatalf("test setup failed: %q", got)
	}
	var m slackProviderModel
	m.UserToken = types.StringNull()
	if resolved := resolveToken(m.UserToken, "SLACK_USER_TOKEN"); resolved != "xoxp-from-env" {
		t.Errorf("resolveToken = %q, want the env value when config is null", resolved)
	}

	m.UserToken = types.StringValue("xoxp-explicit")
	if resolved := resolveToken(m.UserToken, "SLACK_USER_TOKEN"); resolved != "xoxp-explicit" {
		t.Errorf("resolveToken = %q, want explicit config to win over env", resolved)
	}
}
