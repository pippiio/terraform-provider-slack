package provider

// Guardrail A-10: this provider was derived from HashiCorp's HashiCups tutorial, and
// copy-paste artefacts survived into user-visible diagnostics. These tests pin the
// corrected strings and prevent the class of defect from returning.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"terraform-provider-slack/internal/slackclient"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// notAClient is deliberately the wrong type for a Configure call.
type notAClient struct{}

func TestMessageResourceConfigure_WrongTypeNamesSlackClient(t *testing.T) {
	r := &messageResource{}
	resp := &resource.ConfigureResponse{}
	r.Configure(context.Background(), resource.ConfigureRequest{ProviderData: &notAClient{}}, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("a wrong ProviderData type must produce a diagnostic")
	}
	d := resp.Diagnostics.Errors()[0]

	if strings.Contains(strings.ToLower(d.Detail()), "hashicups") {
		t.Errorf("detail still references HashiCups: %s", d.Detail())
	}
	if !strings.Contains(d.Detail(), "*slackclient.Client") {
		t.Errorf("detail should name the expected type, got: %s", d.Detail())
	}
	// This is a Resource, not a Data Source -- the summary was copy-pasted.
	if strings.Contains(d.Summary(), "Data Source") {
		t.Errorf("resource diagnostic calls itself a Data Source: %s", d.Summary())
	}
}

func TestUserIdsDataSourceConfigure_WrongTypeNamesSlackClient(t *testing.T) {
	d := &userIdDataSource{}
	resp := &datasource.ConfigureResponse{}
	d.Configure(context.Background(), datasource.ConfigureRequest{ProviderData: &notAClient{}}, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("a wrong ProviderData type must produce a diagnostic")
	}
	diag := resp.Diagnostics.Errors()[0]

	if strings.Contains(strings.ToLower(diag.Detail()), "hashicups") {
		t.Errorf("detail still references HashiCups: %s", diag.Detail())
	}
	if !strings.Contains(diag.Detail(), "*slackclient.Client") {
		t.Errorf("detail should name the expected type, got: %s", diag.Detail())
	}
}

func TestUserDataSourceConfigure_WrongTypeNamesSlackClient(t *testing.T) {
	d := &userDataSource{}
	resp := &datasource.ConfigureResponse{}
	d.Configure(context.Background(), datasource.ConfigureRequest{ProviderData: &notAClient{}}, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("a wrong ProviderData type must produce a diagnostic")
	}
	if !strings.Contains(resp.Diagnostics.Errors()[0].Detail(), "*slackclient.Client") {
		t.Errorf("detail should name the expected type, got: %s", resp.Diagnostics.Errors()[0].Detail())
	}
}

// A valid client container must configure without a diagnostic. The message resource
// uses the bot client -- only user-group management needs the user token.
func TestConfigure_AcceptsRealClient(t *testing.T) {
	host, token := "https://slack.example", "xoxb-test"
	c, err := slackclient.NewClient(&host, &token)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	r := &messageResource{}
	resp := &resource.ConfigureResponse{}
	r.Configure(context.Background(), resource.ConfigureRequest{
		ProviderData: &providerClients{Bot: c},
	}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("a valid client must configure cleanly: %v", resp.Diagnostics)
	}
	if r.client != c {
		t.Error("bot client was not stored on the resource")
	}
}

// The delete failure path described a HashiCups *order*, not a Slack message.
func TestMessageResourceDelete_ErrorNamesSlackMessage(t *testing.T) {
	ctx := context.Background()
	r := &messageResource{client: newStubClient(t, map[string]stub{
		"/api/chat.delete": fixture("err_invalid_auth.json"),
	})}

	state := messageStateWith(t, r, "C123456789", "C123456789", "1503435956.000247")
	resp := &resource.DeleteResponse{State: state}
	r.Delete(ctx, resource.DeleteRequest{State: state}, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("a failing delete must produce a diagnostic")
	}
	d := resp.Diagnostics.Errors()[0]

	combined := strings.ToLower(d.Summary() + " " + d.Detail())
	if strings.Contains(combined, "hashicups") {
		t.Errorf("delete diagnostic still references HashiCups: %s / %s", d.Summary(), d.Detail())
	}
	if strings.Contains(combined, "order") {
		t.Errorf("delete diagnostic calls a Slack message an 'order': %s / %s", d.Summary(), d.Detail())
	}
	if !strings.Contains(combined, "message") {
		t.Errorf("delete diagnostic should say what it failed to delete: %s / %s", d.Summary(), d.Detail())
	}
}

// Repo-wide guard: no Go source may mention HashiCups. This is the rule from guardrail
// A-10 made executable, so the artefacts cannot silently return via another copy-paste.
func TestNoHashiCupsReferencesInGoSource(t *testing.T) {
	roots := []string{filepath.Join("..", "..", "internal"), filepath.Join("..", "..", "main.go")}
	self := "no_tutorial_leftovers_test.go"

	for _, root := range roots {
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, self) {
				return nil
			}
			b, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			if strings.Contains(strings.ToLower(string(b)), "hashicups") {
				t.Errorf("%s references HashiCups -- see guardrail A-10", path)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", root, err)
		}
	}
}
