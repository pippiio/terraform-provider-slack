package provider

// Story: provider host defaulting and message sensitivity.
//
// `host` exists so the client can be pointed at a stub -- Client.Host is injectable
// with no default (architecture.md I-5). That injectability lives in the client; it
// does not require every operator to spell out Slack's own address. The default is
// applied here, at the provider boundary, so the client stays as testable as before.

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// providerConfigOf builds a tfsdk.Config for the provider schema. A nil pointer means
// the attribute is absent from configuration.
func providerConfigOf(t *testing.T, p *slackProvider, host, token, userToken *string) tfsdk.Config {
	t.Helper()
	ctx := context.Background()

	schemaResp := &provider.SchemaResponse{}
	p.Schema(ctx, provider.SchemaRequest{}, schemaResp)
	if schemaResp.Diagnostics.HasError() {
		t.Fatalf("building provider schema: %v", schemaResp.Diagnostics)
	}
	sch := schemaResp.Schema

	val := func(s *string) tftypes.Value {
		if s == nil {
			return tftypes.NewValue(tftypes.String, nil)
		}
		return tftypes.NewValue(tftypes.String, *s)
	}

	objType := sch.Type().TerraformType(ctx).(tftypes.Object)
	return tfsdk.Config{Schema: sch, Raw: tftypes.NewValue(objType, map[string]tftypes.Value{
		"host":       val(host),
		"token":      val(token),
		"user_token": val(userToken),
	})}
}

func configureProvider(t *testing.T, host, token *string) *provider.ConfigureResponse {
	t.Helper()
	// Neither var may leak in from the developer's shell.
	t.Setenv("SLACK_HOST", "")
	t.Setenv("SLACK_USER_TOKEN", "")

	p := &slackProvider{version: "test"}
	resp := &provider.ConfigureResponse{}
	p.Configure(context.Background(),
		provider.ConfigureRequest{Config: providerConfigOf(t, p, host, token, nil)}, resp)
	return resp
}

// Omitting host must not be an error: there is one Slack, and its address is not a
// detail every configuration should have to repeat.
func TestProviderConfigure_DefaultsHostToSlack(t *testing.T) {
	tok := "xoxb-test-token"
	resp := configureProvider(t, nil, &tok)

	if resp.Diagnostics.HasError() {
		t.Fatalf("omitting host must not fail: %v", resp.Diagnostics)
	}

	clients, ok := resp.ResourceData.(*providerClients)
	if !ok {
		t.Fatalf("ResourceData is %T, want *providerClients", resp.ResourceData)
	}
	if got := clients.Bot.Host; got != defaultSlackHost {
		t.Errorf("Bot.Host = %q, want the default %q", got, defaultSlackHost)
	}
}

// The default must not override an explicit host, or the stub harness breaks.
func TestProviderConfigure_ExplicitHostWins(t *testing.T) {
	host, tok := "http://127.0.0.1:1", "xoxb-test-token"
	resp := configureProvider(t, &host, &tok)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	clients := resp.ResourceData.(*providerClients)
	if got := clients.Bot.Host; got != host {
		t.Errorf("Bot.Host = %q, want the configured %q", got, host)
	}
}

// A missing token is still a hard error -- defaulting the host must not soften it.
func TestProviderConfigure_MissingTokenStillFails(t *testing.T) {
	resp := configureProvider(t, nil, nil)

	if !resp.Diagnostics.HasError() {
		t.Fatal("a missing bot token must still fail")
	}
}

// The message text can carry anything the operator sends to people -- credentials in
// an onboarding announcement, unreleased news. Terraform must not print it in a plan.
func TestMessageResourceSchema_MessageIsSensitive(t *testing.T) {
	r := &messageResource{}
	resp := &resource.SchemaResponse{}
	r.Schema(context.Background(), resource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("building schema: %v", resp.Diagnostics)
	}

	attr, ok := resp.Schema.Attributes["message"]
	if !ok {
		t.Fatal("schema has no message attribute")
	}
	if !attr.IsSensitive() {
		t.Error("message is not marked sensitive, so terraform plan prints it in full")
	}
}
