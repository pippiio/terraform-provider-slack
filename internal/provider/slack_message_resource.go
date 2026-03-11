package provider

import (
	"context"
	"fmt"
	"terraform-provider-slack/internal/slackclient"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource              = &messageResource{}
	_ resource.ResourceWithConfigure = &messageResource{}
)

func NewMessageResource() resource.Resource {
	return &messageResource{}
}

type messageResource struct {
	client *slackclient.Client
}

type messageResourceModel struct {
	Message     types.String `tfsdk:"message"`
	Slack_IDs   types.Set    `tfsdk:"slack_ids"`
	LastUpdated types.String `tfsdk:"last_updated"`
	Ts          types.List   `tfsdk:"ts"`
}

func (r *messageResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_message"
}

func (r *messageResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"message": schema.StringAttribute{
				Required: true,
			},
			"slack_ids": schema.SetAttribute{
				ElementType: types.StringType,
				Required:    true,
			},
			"last_updated": schema.StringAttribute{
				Computed: true,
			},
			"ts": schema.ListAttribute{
				ElementType: types.StringType,
				Computed:    true,
			},
		},
	}
}

func (r *messageResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*slackclient.Client)

	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Data Source Configure Type",
			fmt.Sprintf("Expected *hashicups.Client, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	r.client = client
}

func (r *messageResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan messageResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	var tsSlice []attr.Value

	for _, v := range plan.Slack_IDs.Elements() {

		apiresp, err := r.client.SendMessage(plan.Message.ValueString(), v.(types.String).ValueString())
		if err != nil {
			resp.Diagnostics.AddError(
				"Error send message",
				"Could not send message, unexpected error: "+err.Error(),
			)
			return
		}
		tsSlice = append(tsSlice, types.StringValue(apiresp.Ts))
	}

	plan.Ts = types.ListValueMust(types.StringType, tsSlice)
	plan.LastUpdated = types.StringValue(time.Now().Format(time.RFC850))

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

}

func (r *messageResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
}

func (r *messageResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
}

func (r *messageResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
}
